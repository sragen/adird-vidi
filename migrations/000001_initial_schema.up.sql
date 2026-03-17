-- ADIRD — Initial Schema
-- Migration: 000001_initial_schema.up.sql

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ─── Users (Passengers) ───────────────────────────────────────────
CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone       VARCHAR(20) UNIQUE NOT NULL,
    name        VARCHAR(100) NOT NULL DEFAULT '',
    fcm_token   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_users_phone ON users(phone);

-- ─── Drivers ──────────────────────────────────────────────────────
CREATE TABLE drivers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone               VARCHAR(20) UNIQUE NOT NULL,
    name                VARCHAR(100) NOT NULL DEFAULT '',
    vehicle_type        VARCHAR(10) NOT NULL CHECK (vehicle_type IN ('motor', 'car')),
    plate_number        VARCHAR(15) UNIQUE NOT NULL,
    status              VARCHAR(20) NOT NULL DEFAULT 'offline'
                            CHECK (status IN ('offline', 'online', 'on_trip')),
    rating              DECIMAL(3,2) NOT NULL DEFAULT 5.00,
    total_trips         INT NOT NULL DEFAULT 0,
    cancellation_score  DECIMAL(4,3) NOT NULL DEFAULT 0.0,
    fcm_token           TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_drivers_status ON drivers(status) WHERE status != 'offline';
CREATE INDEX idx_drivers_phone  ON drivers(phone);

-- ─── Trips ────────────────────────────────────────────────────────
CREATE TABLE trips (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    passenger_id    UUID NOT NULL REFERENCES users(id),
    driver_id       UUID REFERENCES drivers(id),
    status          VARCHAR(20) NOT NULL DEFAULT 'searching'
                        CHECK (status IN (
                            'searching', 'accepted', 'en_route',
                            'arrived', 'ongoing', 'completed', 'cancelled'
                        )),
    -- Pickup
    pickup_lat      DECIMAL(10,7) NOT NULL,
    pickup_lng      DECIMAL(10,7) NOT NULL,
    pickup_address  TEXT NOT NULL DEFAULT '',
    -- Dropoff
    dropoff_lat     DECIMAL(10,7) NOT NULL,
    dropoff_lng     DECIMAL(10,7) NOT NULL,
    dropoff_address TEXT NOT NULL DEFAULT '',
    -- Fare
    base_fare           DECIMAL(10,2),
    distance_meters     INT,
    duration_seconds    INT,
    surge_multiplier    DECIMAL(3,2) NOT NULL DEFAULT 1.0,
    final_fare          DECIMAL(10,2),
    planned_route_polyline TEXT,
    -- Timestamps
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    driver_accepted_at  TIMESTAMPTZ,
    driver_arrived_at   TIMESTAMPTZ,
    started_at          TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    cancelled_at        TIMESTAMPTZ,
    cancelled_by        VARCHAR(20),
    cancel_reason       TEXT
);
CREATE INDEX idx_trips_passenger ON trips(passenger_id, created_at DESC);
CREATE INDEX idx_trips_driver    ON trips(driver_id, created_at DESC);
CREATE INDEX idx_trips_status    ON trips(status)
    WHERE status NOT IN ('completed', 'cancelled');

-- ─── Trip GPS Trace ───────────────────────────────────────────────
CREATE TABLE trip_locations (
    id          BIGSERIAL PRIMARY KEY,
    trip_id     UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    lat         DECIMAL(10,7) NOT NULL,
    lng         DECIMAL(10,7) NOT NULL,
    speed       DECIMAL(5,2),
    heading     SMALLINT,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_trip_locations_trip ON trip_locations(trip_id, recorded_at);

-- ─── ETA Learning ─────────────────────────────────────────────────
CREATE TABLE eta_corrections (
    id                      BIGSERIAL PRIMARY KEY,
    origin_grid             VARCHAR(20),
    dest_grid               VARCHAR(20),
    hour_of_day             SMALLINT CHECK (hour_of_day BETWEEN 0 AND 23),
    day_of_week             SMALLINT CHECK (day_of_week BETWEEN 0 AND 6),
    osrm_estimate_seconds   INT,
    actual_seconds          INT,
    multiplier              DECIMAL(4,3),
    trip_id                 UUID,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_eta_corrections_lookup
    ON eta_corrections(origin_grid, dest_grid, hour_of_day, day_of_week);

-- ─── Payments ─────────────────────────────────────────────────────
CREATE TABLE payments (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id                 UUID NOT NULL REFERENCES trips(id),
    amount                  DECIMAL(10,2) NOT NULL,
    method                  VARCHAR(20) NOT NULL
                                CHECK (method IN ('cash', 'qris', 'gopay', 'ovo', 'dana')),
    status                  VARCHAR(20) NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending', 'paid', 'failed', 'refunded')),
    midtrans_order_id       VARCHAR(100),
    midtrans_transaction_id VARCHAR(100),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at              TIMESTAMPTZ
);
CREATE INDEX idx_payments_trip ON payments(trip_id);

-- ─── Ratings ──────────────────────────────────────────────────────
CREATE TABLE ratings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    trip_id     UUID NOT NULL REFERENCES trips(id),
    rater_type  VARCHAR(10) NOT NULL CHECK (rater_type IN ('passenger', 'driver')),
    rater_id    UUID NOT NULL,
    ratee_id    UUID NOT NULL,
    score       SMALLINT NOT NULL CHECK (score BETWEEN 1 AND 5),
    comment     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_ratings_trip_rater ON ratings(trip_id, rater_type);

-- ─── Fare Config ──────────────────────────────────────────────────
CREATE TABLE fare_configs (
    id              BIGSERIAL PRIMARY KEY,
    vehicle_type    VARCHAR(10) NOT NULL,
    base_fare       DECIMAL(10,2) NOT NULL,
    per_km_rate     DECIMAL(10,2) NOT NULL,
    per_min_rate    DECIMAL(10,2) NOT NULL,
    min_fare        DECIMAL(10,2) NOT NULL,
    effective_from  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    effective_to    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed: initial Jakarta rates
INSERT INTO fare_configs (vehicle_type, base_fare, per_km_rate, per_min_rate, min_fare) VALUES
    ('motor', 5000,  2000, 300, 10000),
    ('car',   8000,  4000, 500, 20000);
