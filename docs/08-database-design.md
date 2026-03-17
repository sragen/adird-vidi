---
title: Database Design
tags: [database, postgresql, redis, schema]
created: 2026-03-16
---

# Database Design

> **See also**: [[02-system-architecture]] | [[05-realtime-tracking]] | [[09-infrastructure]]

---

## Data Store Decision Matrix

| Data Type | Store | Reason |
|-----------|-------|--------|
| Users, drivers, trips | **PostgreSQL** | ACID, joins, permanent records |
| Payments, ratings | **PostgreSQL** | Financial integrity, audit trail |
| ETA learning data | **PostgreSQL** | Historical queries, aggregations |
| Live driver locations | **Redis GEO** | O(log N) geo queries, sub-ms |
| Driver online status | **Redis HASH** | Fast reads, auto-expire TTL |
| Active trip state | **Redis JSON** | Hot path, 1h TTL |
| Dispatch offer locks | **Redis SET NX** | Atomic locking, 20s TTL |
| Surge demand counters | **Redis INCR** | Fast counters, 5min TTL |
| Event streams | **Not yet** | Justified only at >1000 drivers |

---

## PostgreSQL Schema

### Extensions

```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "postgis";   -- optional: geo queries in PG (use Redis instead for MVP)
```

### Users (Passengers)

```sql
CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone       VARCHAR(20) UNIQUE NOT NULL,  -- format: +628xx
    name        VARCHAR(100),
    fcm_token   TEXT,                         -- Firebase push notification token
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_phone ON users(phone);
```

### Drivers

```sql
CREATE TABLE drivers (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone              VARCHAR(20) UNIQUE NOT NULL,
    name               VARCHAR(100),
    vehicle_type       VARCHAR(10) NOT NULL
                           CHECK (vehicle_type IN ('motor', 'car')),
    plate_number       VARCHAR(15) UNIQUE NOT NULL,
    status             VARCHAR(20) DEFAULT 'offline'
                           CHECK (status IN ('offline', 'online', 'on_trip')),
    rating             DECIMAL(3,2) DEFAULT 5.00,
    total_trips        INT DEFAULT 0,
    cancellation_score DECIMAL(4,3) DEFAULT 0.0,  -- 0.0-1.0 range
    fcm_token          TEXT,
    -- KTP verification (future Dukcapil integration)
    nik                VARCHAR(16),
    ktp_verified       BOOLEAN DEFAULT FALSE,
    ktp_photo_url      TEXT,
    created_at         TIMESTAMPTZ DEFAULT NOW(),
    updated_at         TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_drivers_status ON drivers(status) WHERE status != 'offline';
CREATE INDEX idx_drivers_phone  ON drivers(phone);
```

### Trips

```sql
CREATE TABLE trips (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    passenger_id UUID REFERENCES users(id),
    driver_id    UUID REFERENCES drivers(id),

    -- State machine
    status VARCHAR(20) NOT NULL DEFAULT 'searching'
        CHECK (status IN (
            'searching',   -- waiting for driver
            'accepted',    -- driver accepted
            'en_route',    -- driver heading to pickup
            'arrived',     -- driver at pickup location
            'ongoing',     -- trip in progress
            'completed',   -- trip finished
            'cancelled'    -- cancelled by passenger or driver
        )),

    -- Pickup
    pickup_lat     DECIMAL(10,7) NOT NULL,
    pickup_lng     DECIMAL(10,7) NOT NULL,
    pickup_address TEXT,

    -- Dropoff
    dropoff_lat     DECIMAL(10,7) NOT NULL,
    dropoff_lng     DECIMAL(10,7) NOT NULL,
    dropoff_address TEXT,

    -- Fare
    base_fare         DECIMAL(10,2),
    distance_meters   INT,
    duration_seconds  INT,
    surge_multiplier  DECIMAL(3,2) DEFAULT 1.0,
    final_fare        DECIMAL(10,2),

    -- Route snapshot (OSRM polyline, for navigation replay and disputes)
    planned_route_polyline TEXT,

    -- Timing
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    driver_accepted_at  TIMESTAMPTZ,
    driver_arrived_at   TIMESTAMPTZ,
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    cancelled_at        TIMESTAMPTZ,
    cancelled_by        VARCHAR(20),  -- 'passenger' | 'driver' | 'system'
    cancel_reason       TEXT
);

CREATE INDEX idx_trips_passenger ON trips(passenger_id, created_at DESC);
CREATE INDEX idx_trips_driver    ON trips(driver_id, created_at DESC);
-- Partial index: only active trips (most queries hit active ones)
CREATE INDEX idx_trips_status    ON trips(status)
    WHERE status NOT IN ('completed', 'cancelled');
```

### Trip GPS Trace

```sql
-- Stores GPS breadcrumb trail for each trip
-- Used for: dispute resolution, ETA learning, route replay
CREATE TABLE trip_locations (
    id          BIGSERIAL PRIMARY KEY,
    trip_id     UUID REFERENCES trips(id) ON DELETE CASCADE,
    lat         DECIMAL(10,7) NOT NULL,
    lng         DECIMAL(10,7) NOT NULL,
    speed       DECIMAL(5,2),
    heading     SMALLINT,
    recorded_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_trip_locations_trip ON trip_locations(trip_id, recorded_at);
-- Partition by month after 6 months of data accumulation
```

### ETA Corrections (Learning Table)

```sql
CREATE TABLE eta_corrections (
    id                    BIGSERIAL PRIMARY KEY,
    origin_grid           VARCHAR(20),   -- 200m grid cell hash
    dest_grid             VARCHAR(20),
    hour_of_day           SMALLINT CHECK (hour_of_day BETWEEN 0 AND 23),
    day_of_week           SMALLINT CHECK (day_of_week BETWEEN 0 AND 6),
    osrm_estimate_seconds INT,
    actual_seconds        INT,
    multiplier            DECIMAL(4,3),  -- actual / estimated (0.5-4.0 valid range)
    trip_id               UUID,
    created_at            TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_eta_corrections_lookup
    ON eta_corrections(origin_grid, dest_grid, hour_of_day, day_of_week);
```

### Payments

```sql
CREATE TABLE payments (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id                UUID REFERENCES trips(id),
    amount                 DECIMAL(10,2) NOT NULL,
    method                 VARCHAR(20) NOT NULL
                               CHECK (method IN ('cash', 'qris', 'gopay', 'ovo', 'dana')),
    status                 VARCHAR(20) DEFAULT 'pending'
                               CHECK (status IN ('pending', 'paid', 'failed', 'refunded')),
    midtrans_order_id      VARCHAR(100),       -- for QRIS/e-wallet payments
    midtrans_transaction_id VARCHAR(100),
    created_at             TIMESTAMPTZ DEFAULT NOW(),
    settled_at             TIMESTAMPTZ
);

CREATE INDEX idx_payments_trip ON payments(trip_id);
```

### Ratings

```sql
CREATE TABLE ratings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id     UUID REFERENCES trips(id),
    rater_type  VARCHAR(10) NOT NULL CHECK (rater_type IN ('passenger', 'driver')),
    rater_id    UUID NOT NULL,
    ratee_id    UUID NOT NULL,
    score       SMALLINT NOT NULL CHECK (score BETWEEN 1 AND 5),
    comment     TEXT,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Prevent duplicate ratings for same trip
CREATE UNIQUE INDEX idx_ratings_trip_rater ON ratings(trip_id, rater_type);
```

### Fare Configuration (Editable Without Redeploy)

```sql
CREATE TABLE fare_configs (
    id              BIGSERIAL PRIMARY KEY,
    vehicle_type    VARCHAR(10) NOT NULL,  -- 'motor', 'car'
    base_fare       DECIMAL(10,2) NOT NULL,
    per_km_rate     DECIMAL(10,2) NOT NULL,
    per_min_rate    DECIMAL(10,2) NOT NULL,
    min_fare        DECIMAL(10,2) NOT NULL,
    effective_from  TIMESTAMPTZ DEFAULT NOW(),
    effective_to    TIMESTAMPTZ,           -- NULL = currently active
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Current rates (initial values for Jakarta)
INSERT INTO fare_configs (vehicle_type, base_fare, per_km_rate, per_min_rate, min_fare) VALUES
  ('motor', 5000, 2000, 300, 10000),   -- Rp 5K base + Rp 2K/km + Rp 300/min, min Rp 10K
  ('car',   8000, 4000, 500, 20000);   -- Rp 8K base + Rp 4K/km + Rp 500/min, min Rp 20K
```

---

## Redis Key Reference

```bash
# ─── Live Driver Tracking ───

# Geospatial index of all online drivers (auto-cleaned every 10s)
GEOADD drivers:online <lng> <lat> <driver_id>

# Driver metadata hash (60s TTL = auto-offline if no heartbeat)
HSET driver:<id> status online lat <lat> lng <lng> speed <kmh> heading <deg> updated_at <unix>
EXPIRE driver:<id> 60


# ─── Trip State (Hot Path) ───

# Active trip state snapshot (1h TTL, avoids DB hit on every WS update)
SET trip:<id> '{"status":"ongoing","driver_id":"...","passenger_id":"...","pickup":...}' EX 3600


# ─── Dispatch ───

# Offer lock: prevents same driver receiving two simultaneous offers (atomic SET NX)
SET dispatch:lock:<driver_id> <order_id> NX EX 20

# Trips completed by driver in last hour (for fairness scoring)
INCR driver:trips_hour:<driver_id>
EXPIRE driver:trips_hour:<driver_id> 3600

# Cancellation penalty (decays over 24h)
INCRBY driver:cancel_penalty:<driver_id> 10
EXPIRE driver:cancel_penalty:<driver_id> 86400

# Driver rejection count today
INCR driver:rejections_today:<driver_id>
EXPIRE driver:rejections_today:<driver_id> 86400


# ─── Surge Pricing ───

# Pending order count in zone (expires when orders are resolved)
INCR zone:demand:<zone_id>
EXPIRE zone:demand:<zone_id> 300


# ─── Location Streaming ───

# Pub/sub channel: passenger subscribes to watch their driver
PUBLISH channel:driver:<driver_id> <location_json>
SUBSCRIBE channel:driver:<driver_id>


# ─── Routing Cache ───

# Route result (1h TTL)
SET route:<origin_grid>:<dest_grid> <route_json> EX 3600

# ETA-only result (30min TTL)
SET eta:<origin_grid>:<dest_grid> <seconds> EX 1800


# ─── Auth ───

# OTP code (5min TTL)
SET otp:<phone> <6_digit_code> EX 300

# Refresh token → user ID mapping
SET refresh:<token_hash> <user_id> EX 2592000  # 30 days

# Revoked tokens (blacklist)
SADD revoked_tokens <token_hash>
```

---

## Fare Calculation Logic

```go
func CalculateFare(config FareConfig, distanceMeters int, durationSeconds int, surge float64) Fare {
    distanceKm := float64(distanceMeters) / 1000.0
    durationMin := float64(durationSeconds) / 60.0

    base     := config.BaseFare
    distance := distanceKm * config.PerKmRate
    time     := durationMin * config.PerMinRate
    subtotal := base + distance + time

    if subtotal < config.MinFare {
        subtotal = config.MinFare
    }

    final := subtotal * surge

    return Fare{
        BaseFare:        base,
        DistanceCharge:  distance,
        TimeCharge:      time,
        SurgeMultiplier: surge,
        Total:           math.Round(final/500) * 500, // round to nearest Rp 500
    }
}
```

---

## Database Migrations (golang-migrate)

```
migrations/
  001_initial_schema.sql         -- users, drivers, trips, payments, ratings
  002_eta_corrections.sql        -- eta_corrections table
  003_fare_configs.sql           -- fare_configs table + initial data
  004_driver_ktp_fields.sql      -- NIK, ktp_verified fields on drivers
  005_trip_locations.sql         -- GPS trace table
```

```bash
# Run migrations
migrate -path migrations -database "postgres://..." up

# Check current version
migrate -path migrations -database "postgres://..." version

# Rollback last migration
migrate -path migrations -database "postgres://..." down 1
```

---

## Backup Strategy

```bash
# Daily PostgreSQL backup to Hetzner Storage Box (cheap object storage)
# Add to cron: 0 2 * * * /scripts/backup_db.sh

pg_dump adird_db | gzip > /backups/adird-$(date +%Y%m%d).sql.gz
# Upload to Hetzner Storage Box via rsync or rclone
rclone copy /backups/ remote:adird-backups/

# Keep 30 days of backups
find /backups/ -name "*.sql.gz" -mtime +30 -delete
```

---

*See [[09-infrastructure]] for PostgreSQL hosting decision, [[05-realtime-tracking]] for Redis usage patterns.*
