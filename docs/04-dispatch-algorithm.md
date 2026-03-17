---
title: Dispatch Algorithm Design
tags: [dispatch, algorithm, go, redis]
created: 2026-03-16
---

# Dispatch Algorithm Design

> **See also**: [[02-system-architecture]] | [[05-realtime-tracking]] | [[06-eta-prediction]]

---

## Overview

```
New Order Created
      │
      ▼
Step 1: FindCandidates()     ← Redis GEOSEARCH (5km radius, top 10)
      │
      ▼
Step 2: ScoreAndRank()       ← ETA × 0.6 + Distance × 0.3 + Fairness × 0.1
      │
      ▼
Step 3: OfferRide()          ← WebSocket offer, 15s timeout, Redis NX lock
      │
   accepted? ──No──► Try next candidate
      │
     Yes
      │
      ▼
AssignDriver()               ← Update trip state, notify passenger
```

---

## Step 1: Candidate Discovery via Redis GEOSEARCH

```go
func (e *DispatchEngine) FindCandidates(ctx context.Context, pickup LatLng) ([]Driver, error) {
    candidates, err := e.redis.GeoSearch(ctx, "drivers:online", &redis.GeoSearchQuery{
        Longitude:  pickup.Lng,
        Latitude:   pickup.Lat,
        Radius:     5,
        RadiusUnit: "km",
        Count:      10,
        Sort:       "ASC", // nearest first
    }).Result()
    if err != nil {
        return nil, fmt.Errorf("geosearch: %w", err)
    }

    drivers := make([]Driver, 0, len(candidates))
    for _, c := range candidates {
        data, err := e.redis.HGetAll(ctx, "driver:"+c.Name).Result()
        if err != nil || data["status"] != "online" {
            continue // skip offline or expired drivers
        }
        drivers = append(drivers, Driver{
            ID:               c.Name,
            Lat:              parseFloat(data["lat"]),
            Lng:              parseFloat(data["lng"]),
            DistanceToPickup: c.Dist,
        })
    }
    return drivers, nil
}
```

---

## Step 2: Scored Dispatch (ETA + Fairness Weighting)

### Score Formula
```
score = (eta_seconds × 0.6) + (distance_km × 0.3) + (trips_last_hour × 0.1) + (cancel_penalty × 50)
```
**Lower score = better candidate.**

### Implementation

```go
func (e *DispatchEngine) ScoreAndRank(ctx context.Context, candidates []Driver, order Order) ([]ScoredDriver, error) {
    scored := make([]ScoredDriver, 0, len(candidates))

    for _, d := range candidates {
        // Get ETA from OSRM via prediction module
        eta, err := e.eta.Predict(ctx, LatLng{d.Lat, d.Lng}, order.Pickup)
        if err != nil {
            // Fallback: crude distance/speed estimate
            eta = time.Duration(d.DistanceToPickup/10) * time.Minute
        }

        // Fairness: penalize drivers who got too many recent trips
        tripsLastHour, _ := e.redis.Get(ctx,
            fmt.Sprintf("driver:trips_hour:%s", d.ID)).Int()

        // Cancel penalty: increases dispatch score (makes driver less likely to be offered)
        cancelPenalty, _ := e.redis.Get(ctx,
            fmt.Sprintf("driver:cancel_penalty:%s", d.ID)).Float64()

        score := (eta.Seconds() * 0.6) +
                 (d.DistanceToPickup * 0.3) +
                 (float64(tripsLastHour) * 0.1) +
                 (cancelPenalty * 50.0)

        scored = append(scored, ScoredDriver{
            Driver: d,
            Score:  score,
            ETA:    eta,
        })
    }

    sort.Slice(scored, func(i, j int) bool {
        return scored[i].Score < scored[j].Score
    })
    return scored, nil
}
```

---

## Step 3: Offer with 15-Second Timeout

### The Offer Lock Pattern

Uses Redis `SET NX EX` to prevent the same driver from receiving two simultaneous offers (can happen if dispatch is called twice during peak load):

```go
func (e *DispatchEngine) OfferRide(ctx context.Context, driverID string, order Order) (bool, error) {
    lockKey := fmt.Sprintf("dispatch:lock:%s", driverID)

    // Atomic: only set if key doesn't exist, expires in 20s
    set, err := e.redis.SetNX(ctx, lockKey, order.ID, 20*time.Second).Result()
    if err != nil {
        return false, fmt.Errorf("redis setnx: %w", err)
    }
    if !set {
        return false, ErrDriverBusy // driver already has a pending offer
    }

    // Register response channel BEFORE sending offer (avoid race condition)
    respChan := make(chan OfferResponse, 1)
    e.mu.Lock()
    e.offerResponses[driverID] = respChan
    e.mu.Unlock()

    defer func() {
        e.redis.Del(ctx, lockKey)
        e.mu.Lock()
        delete(e.offerResponses, driverID)
        e.mu.Unlock()
    }()

    // Push offer to driver via WebSocket
    e.hub.SendToDriver(driverID, OfferMessage{
        OrderID:      order.ID,
        Pickup:       order.Pickup,
        Dropoff:      order.Dropoff,
        FareEstimate: order.FareEstimate,
        ExpiresIn:    15, // seconds shown on driver countdown UI
    })

    // Wait for response with timeout
    select {
    case resp := <-respChan:
        return resp.Accepted, nil
    case <-time.After(15 * time.Second):
        return false, ErrOfferTimeout
    }
}
```

---

## Step 4: Full Dispatch Loop

```go
func (e *DispatchEngine) Dispatch(ctx context.Context, order Order) error {
    candidates, err := e.FindCandidates(ctx, order.Pickup)
    if err != nil || len(candidates) == 0 {
        return e.notifyNoDrivers(ctx, order)
    }

    scored, err := e.ScoreAndRank(ctx, candidates, order)
    if err != nil {
        return err
    }

    for _, candidate := range scored {
        // Check if passenger cancelled while we were searching
        currentStatus, _ := e.redis.HGet(ctx, "trip:"+order.ID, "status").Result()
        if currentStatus == "cancelled" {
            return nil // abort dispatch, passenger already cancelled
        }

        accepted, err := e.OfferRide(ctx, candidate.Driver.ID, order)
        if err != nil {
            // ErrDriverBusy or ErrOfferTimeout: silently try next
            continue
        }
        if accepted {
            return e.assignDriver(ctx, order, candidate.Driver)
        }

        // Driver rejected: log rejection for analytics
        e.redis.Incr(ctx, fmt.Sprintf("driver:rejections_today:%s", candidate.Driver.ID))
    }

    // All candidates exhausted: widen search radius once and retry
    return e.retryWithWiderRadius(ctx, order)
}

func (e *DispatchEngine) retryWithWiderRadius(ctx context.Context, order Order) error {
    order.SearchRadius = 8.0 // km (expanded from 5km)
    candidates, err := e.FindCandidates(ctx, order.Pickup)
    if err != nil || len(candidates) == 0 {
        return e.notifyNoDrivers(ctx, order)
    }

    scored, _ := e.ScoreAndRank(ctx, candidates, order)
    for _, candidate := range scored {
        accepted, err := e.OfferRide(ctx, candidate.Driver.ID, order)
        if err != nil { continue }
        if accepted { return e.assignDriver(ctx, order, candidate.Driver) }
    }

    // Truly no drivers available
    return e.notifyNoDrivers(ctx, order)
}
```

---

## Surge Pricing

### Zone-Based Supply/Demand Ratio

```go
func (e *DispatchEngine) calculateSurge(ctx context.Context, zoneID string) float64 {
    // Demand: pending orders in zone (counter expires after 5 min)
    demandStr, _ := e.redis.Get(ctx, "zone:demand:"+zoneID).Result()
    demand, _ := strconv.Atoi(demandStr)

    // Supply: online drivers in zone (GEO radius count)
    zone := e.zones[zoneID]
    supply, _ := e.redis.GeoSearchCount(ctx, "drivers:online", &redis.GeoSearchQuery{
        Longitude:  zone.CenterLng,
        Latitude:   zone.CenterLat,
        Radius:     zone.RadiusKm,
        RadiusUnit: "km",
    }).Result()

    ratio := float64(demand) / float64(supply+1) // +1 prevents divide-by-zero

    switch {
    case ratio > 3.0: return 2.0   // 2x surge
    case ratio > 2.0: return 1.5   // 1.5x surge
    case ratio > 1.5: return 1.2   // 1.2x surge
    default:          return 1.0   // no surge
    }
}
```

### Jakarta Surge Zones (Initial Configuration)

| Zone ID | Name | Center | Radius |
|---------|------|--------|--------|
| `jkt-pusat-sudirman` | Sudirman/SCBD | -6.2231, 106.8072 | 2.5km |
| `jkt-pusat-menteng` | Menteng | -6.1955, 106.8368 | 2km |
| `jkt-selatan-kemang` | Kemang | -6.2614, 106.8142 | 2km |
| `jkt-selatan-blokm` | Blok M | -6.2438, 106.7991 | 1.5km |
| `jkt-pusat-gambir` | Gambir/Monas | -6.1747, 106.8227 | 2km |

---

## Driver Cancellation Handling

```go
func (s *OrderService) DriverCancel(ctx context.Context, tripID, driverID string) error {
    trip := s.getTrip(ctx, tripID)

    // Guard: cannot cancel an ongoing trip
    if trip.Status == "ongoing" {
        return ErrCannotCancelOngoingTrip
    }

    // Reset trip to searching state, clear driver assignment
    s.db.Exec(`
        UPDATE trips SET driver_id=NULL, status='searching',
        driver_accepted_at=NULL, cancelled_by='driver'
        WHERE id=$1 AND driver_id=$2`, tripID, driverID)

    // Cancellation penalty: affects dispatch score for 24 hours
    penaltyKey := fmt.Sprintf("driver:cancel_penalty:%s", driverID)
    s.redis.IncrBy(ctx, penaltyKey, 10)
    s.redis.Expire(ctx, penaltyKey, 24*time.Hour)

    // After 5 cancellations today, flag for manual review
    cancellations, _ := s.redis.Get(ctx,
        fmt.Sprintf("driver:cancellations_today:%s", driverID)).Int()
    if cancellations >= 5 {
        s.flagDriverForReview(ctx, driverID, "excessive_cancellations")
    }

    // Immediately re-dispatch, excluding this driver
    go s.dispatch.DispatchExcluding(ctx, trip, driverID)

    // Notify passenger
    s.notif.Send(ctx, trip.PassengerFCMToken, PushMessage{
        Title: "Mencari driver lain",
        Body:  "Driver sebelumnya tidak bisa menjemput. Mencari driver lain...",
    })
    return nil
}
```

---

## Driver Fairness System

Three mechanisms to prevent driver monopolization:

1. **Trips-per-hour penalty**: Drivers who completed many recent trips get a higher score (less likely to be selected), giving idle drivers more opportunity.

2. **Cancellation penalty**: Drivers who cancel frequently get higher dispatch scores, reducing their offer frequency.

3. **Idle nudge notifications**: A goroutine runs every 5 minutes to push "High demand nearby" messages to idle drivers, helping them self-distribute.

```go
func (s *NudgeService) RunIdleNudges(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        idleDrivers := s.getIdleDriversOver(ctx, 10*time.Minute)
        for _, driverID := range idleDrivers {
            driverLoc := s.getDriverLocation(ctx, driverID)
            zone := s.findHighDemandZoneNear(ctx, driverLoc, 5.0)
            if zone != nil && zone.SurgeMultiplier >= 1.2 {
                s.notif.SendToDriver(driverID, fmt.Sprintf(
                    "Permintaan tinggi di %s (%.1f km). Pindah ke sana?",
                    zone.Name, zone.Distance))
            }
        }
    }
}
```

---

## Batch Dispatch (Future)

At higher scale (1000+ drivers), individual sequential offers become too slow. Batch dispatch sends offers to multiple drivers simultaneously and takes the first acceptance:

```go
// Future: send to top 3 drivers simultaneously
func (e *DispatchEngine) BatchDispatch(ctx context.Context, order Order, candidates []ScoredDriver) {
    ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
    defer cancel()

    resultChan := make(chan string, 3) // driver ID of first acceptor

    for _, c := range candidates[:min(3, len(candidates))] {
        go func(driverID string) {
            accepted, _ := e.OfferRide(ctx, driverID, order)
            if accepted {
                resultChan <- driverID
            }
        }(c.Driver.ID)
    }

    select {
    case winnerID := <-resultChan:
        e.assignDriver(ctx, order, winnerID)
        cancel() // cancel offers to other drivers
    case <-ctx.Done():
        e.notifyNoDrivers(ctx, order)
    }
}
```

---

*See [[05-realtime-tracking]] for WebSocket hub implementation, [[06-eta-prediction]] for ETA calculation.*
