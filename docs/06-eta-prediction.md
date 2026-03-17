---
title: ETA Prediction Model
tags: [eta, prediction, machine-learning, traffic]
created: 2026-03-16
---

# ETA Prediction Model

> **See also**: [[04-dispatch-algorithm]] | [[07-routing-system]] | [[08-database-design]]

---

## Overview

Jakarta traffic is notoriously unpredictable. OSRM's static routing assumes free-flow speeds from the road network profile. Actual travel time in Jakarta:

| Time of Day | OSRM vs Reality |
|-------------|----------------|
| 00:00–06:00 (off-peak) | ~1.1x (OSRM mostly accurate) |
| 07:00–09:30 (morning rush) | **2.0–2.8x** (Sudirman corridor) |
| 12:00–13:00 (lunch rush) | **1.5–1.8x** (SCBD, Kuningan) |
| 17:00–20:00 (evening rush) | **1.8–2.5x** (all major roads) |
| 20:00–24:00 (evening) | ~1.2–1.4x |

The ETA system evolves through 3 phases as the platform collects more trip data.

---

## Phase 1: OSRM Raw Duration (MVP, Day 1)

Use OSRM `/route` duration as base. Apply a hardcoded **1.4x Jakarta correction factor** as an immediate improvement over raw OSRM:

```go
func (p *ETAPredictor) Predict(ctx context.Context, origin, dest LatLng) (time.Duration, error) {
    osrmETA, err := p.routing.ETA(ctx, origin, dest)
    if err != nil {
        return 0, err
    }

    // Phase 1: flat Jakarta correction (actual avg = 1.4x free-flow)
    corrected := time.Duration(float64(osrmETA) * 1.4)
    return corrected, nil
}
```

---

## Phase 2: Time-of-Day Correction Factors (After ~50 Trips)

### Learning Schema

```sql
-- Stores actual vs estimated for each completed trip
CREATE TABLE eta_corrections (
    id BIGSERIAL PRIMARY KEY,
    origin_grid  VARCHAR(20),  -- snapped to 200m grid cell hash
    dest_grid    VARCHAR(20),  -- snapped to 200m grid cell hash
    hour_of_day  SMALLINT,     -- 0-23 (Jakarta WIB)
    day_of_week  SMALLINT,     -- 0=Sun, 1=Mon ... 6=Sat
    osrm_estimate_seconds INT,
    actual_seconds INT,
    multiplier DECIMAL(4,3),   -- actual / osrm_estimated
    trip_id UUID,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX idx_eta_corrections_lookup
    ON eta_corrections(origin_grid, dest_grid, hour_of_day, day_of_week);
```

### Record After Each Trip Completion

```go
func (l *ETALearner) RecordTripCompletion(ctx context.Context, trip Trip) error {
    actual := trip.CompletedAt.Sub(*trip.StartedAt)
    if actual < 30*time.Second {
        return nil // skip suspiciously short trips (data quality)
    }

    osrmETA, _ := l.routing.ETA(ctx,
        LatLng{trip.PickupLat, trip.PickupLng},
        LatLng{trip.DropoffLat, trip.DropoffLng})

    multiplier := actual.Seconds() / osrmETA.Seconds()

    // Filter outliers (accidents, wrong routes) — keep 0.5–4.0 range
    if multiplier < 0.5 || multiplier > 4.0 {
        return nil
    }

    return l.db.ExecContext(ctx, `
        INSERT INTO eta_corrections
          (origin_grid, dest_grid, hour_of_day, day_of_week,
           osrm_estimate_seconds, actual_seconds, multiplier, trip_id)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
        snapToGrid(trip.PickupLat, trip.PickupLng, 200),
        snapToGrid(trip.DropoffLat, trip.DropoffLng, 200),
        trip.StartedAt.Hour(),
        int(trip.StartedAt.Weekday()),
        int(osrmETA.Seconds()),
        int(actual.Seconds()),
        multiplier,
        trip.ID,
    )
}
```

### Prediction with Learned Factors

```go
func (p *ETAPredictor) Predict(ctx context.Context, origin, dest LatLng) (time.Duration, error) {
    osrmETA, err := p.routing.ETA(ctx, origin, dest)
    if err != nil {
        return 0, err
    }

    originGrid := snapToGrid(origin.Lat, origin.Lng, 200)
    destGrid   := snapToGrid(dest.Lat, dest.Lng, 200)
    now        := time.Now().In(jakartaLocation) // Asia/Jakarta (WIB, UTC+7)

    var avgMultiplier float64
    err = p.db.QueryRowContext(ctx, `
        SELECT COALESCE(AVG(multiplier), 1.4)
        FROM (
            SELECT multiplier
            FROM eta_corrections
            WHERE origin_grid = $1
              AND dest_grid   = $2
              AND hour_of_day = $3
              AND day_of_week = $4
            ORDER BY created_at DESC
            LIMIT 50
        ) recent_trips
    `, originGrid, destGrid, now.Hour(), int(now.Weekday())).Scan(&avgMultiplier)

    if err != nil {
        avgMultiplier = 1.4 // fallback if no data
    }

    corrected := time.Duration(float64(osrmETA) * avgMultiplier)
    return corrected, nil
}
```

---

## Phase 3: Live Traffic Heatmap (Post-MVP, ~Month 8)

### Crowdsourced Traffic from Driver GPS

```
100 drivers × 4s GPS update = 25 location points/second
Each point includes: lat, lng, speed, heading, timestamp
```

Aggregate driver speed into a **500m grid cell heatmap**:

```go
func (s *TrafficService) ProcessDriverUpdate(
    ctx context.Context, driverID string, speed, lat, lng float64,
) {
    cellKey := "traffic:cell:" + snapToGrid(lat, lng, 500)
    speedBucket := fmt.Sprintf("%s:speed", cellKey)

    if speed < 5.0 { // near-stationary (< 5km/h)
        slowKey := cellKey + ":slow_count"
        count, _ := s.redis.Incr(ctx, slowKey).Result()
        s.redis.Expire(ctx, slowKey, 3*time.Minute)

        if count >= 3 {
            // 3+ drivers near-stationary in 200m zone → macet
            s.redis.Set(ctx, "macet:"+cellKey, "1", 10*time.Minute)
            s.broadcastTrafficUpdate(ctx, lat, lng, "macet")
        }
    } else {
        // Driver is moving → clear macet flag
        s.redis.Del(ctx, cellKey+":slow_count")
        s.redis.Del(ctx, "macet:"+cellKey)
        // Update rolling average speed for this cell/hour
        s.updateAverageSpeed(ctx, cellKey, speed, time.Now().Hour())
    }
}
```

### Why This Beats Google Maps for Ojek

| Data Source | Car Roads | Motorcycle Alleys | Ojek Behavior |
|-------------|-----------|------------------|---------------|
| Google Maps | ✅ Excellent | ❌ Not tracked | ❌ Ignored |
| HERE Maps | ✅ Good | ❌ Minimal | ❌ Ignored |
| ADIRD | ✅ Good | ✅ **Full coverage** | ✅ **Real data** |

After 6 months, ADIRD has:
- Speed data for every *gang* (alley) motorcycles use
- Actual pickup/dropoff time patterns specific to Jakarta zones
- Motorcycle-specific traffic patterns (different from car traffic)

### Feed into OSRM Custom Speed Table

OSRM supports live traffic injection via custom speed tables:

```bash
# Generate speed table from Redis heatmap (cron job every 30min)
./scripts/generate_speed_table.sh > /data/speeds.csv

# Apply to OSRM without full restart (hot reload)
osrm-customize --segment-speed-file /data/speeds.csv jakarta.osrm
```

---

## Grid Snap Utility

```go
// snapToGrid snaps coordinates to nearest grid cell center
// gridSize in meters (200 for ETA, 500 for traffic)
func snapToGrid(lat, lng float64, gridSizeMeters float64) string {
    // Convert to approximate meters (Jakarta latitude ~6°S)
    metersPerDegLat := 111320.0
    metersPerDegLng := 111320.0 * math.Cos(lat*math.Pi/180)

    gridDegLat := gridSizeMeters / metersPerDegLat
    gridDegLng := gridSizeMeters / metersPerDegLng

    snappedLat := math.Round(lat/gridDegLat) * gridDegLat
    snappedLng := math.Round(lng/gridDegLng) * gridDegLng

    return fmt.Sprintf("%.5f_%.5f", snappedLat, snappedLng)
}
```

---

## ETA Display Strategy

Show ETA to passenger with **honest confidence indicator**:

```
< 50 corrections for this route/hour:  "~12 min"      (approximate)
50–200 corrections:                    "11–13 min"    (range)
> 200 corrections:                     "12 min"       (confident)
```

Always round to nearest minute. Never show seconds to passengers — it creates false precision and increases complaints when off by 90 seconds.

---

*See [[07-routing-system]] for OSRM route queries, [[08-database-design]] for `eta_corrections` table schema.*
