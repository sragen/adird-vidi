---
title: Hard Engineering Problems
tags: [challenges, engineering, jakarta, gps, dispatch]
created: 2026-03-16
---

# Hard Engineering Problems

> **See also**: [[04-dispatch-algorithm]] | [[06-eta-prediction]] | [[05-realtime-tracking]]

---

## 1. Jakarta Traffic Modeling

### The Problem
OSRM gives static free-flow routing. Jakarta's traffic is notoriously dynamic:
- Morning rush (07:00–09:30): Sudirman–Semanggi–Gatot Subroto → speeds at 10–20% of free-flow
- Lunch rush (12:00–13:00): SCBD, Kuningan → 30–40% of free-flow
- Evening rush (17:00–20:00): **All major roads** → 20–40% of free-flow
- Flash floods (Nov–Mar): random road closures, zero advance warning

### Solution: ETA Correction Feedback Loop

See [[06-eta-prediction]] for full implementation.

**Key insight**: After 50+ trip samples per origin/destination grid cell per time-of-day slot, the correction multiplier stabilizes to a reliable estimate. Within 2–3 months of operation, ADIRD's ETA is more accurate than raw OSRM for Jakarta's known congestion patterns.

**Flood handling (future)**: Monitor BPBD Jakarta API (flood alerts) to automatically exclude flooded roads from OSRM routing.

---

## 2. GPS Accuracy in Urban Canyons

### The Problem
Sudirman CBD's glass towers cause GPS multipath reflection:
- Reported position drifts 20–50m from actual position
- Driver appears to be inside Wisma GKBI or floating in the air
- Causes passenger confusion and incorrect "driver arrived" detection

### Solution: OSRM Map Matching

Apply OSRM `/match` API on the driver's last 5 GPS points before:
1. Displaying position to passenger
2. Recording trip trace
3. Detecting "arrived at pickup" (snap to road, then measure distance to pickup)

```go
// In Hub.processLocationUpdate():
snapped := h.mapMatcher.SnapToRoad(msg.Lat, msg.Lng)
// Use snapped.Lat, snapped.Lng instead of raw GPS
```

### Arrived Detection with Map Matching

```go
func isDriverArrivedAtPickup(driverLoc, pickupLoc LatLng) bool {
    // Use map-matched position, not raw GPS
    snappedDriver := osrm.NearestRoad(driverLoc)
    snappedPickup := osrm.NearestRoad(pickupLoc)

    distance := haversine(snappedDriver, snappedPickup)
    return distance < 50.0 // meters
}
```

---

## 3. Dispatch Under Driver Cancellation

### The Problem
A common behavior: driver accepts trip → sees pickup address is in a difficult location → cancels. This creates a re-dispatch delay that frustrates passengers.

**Worse case**: Driver 1 accepts, cancels. Driver 2 accepts, cancels. Passenger waits 90 seconds before third offer.

### Solution: Multi-Layer Defense

**Layer 1: Immediate re-dispatch**
When driver cancels, start new dispatch loop within 500ms.

**Layer 2: Cancellation penalty score**
Cancellation increases `cancel_penalty` (Redis key, 24h decay). Higher penalty = lower dispatch priority = driver receives fewer offers = natural behavior correction.

**Layer 3: Cancellation rate threshold**
```
3 cancellations/day → warning notification to driver
5 cancellations/day → flagged for manual review
10 cancellations/week → temporary suspension (1 day)
```

**Layer 4: Show pickup zone before accepting**
Don't show exact address until accepted (reduces cherry-picking). Show only zone name: "Pickup: Menteng area" in offer screen. Show full address only after acceptance.

---

## 4. Driver Supply Imbalance (Clustering)

### The Problem
Drivers cluster at:
- Blok M Plaza, Grand Indonesia, Plaza Indonesia (mall waiting zones)
- Stasiun Gambir, Stasiun Jakarta Kota (station queues)
- Kampung Melayu Bus Terminal

Meanwhile, residential areas (Kemang, Cipete, Jeruk Purut) have zero driver supply at 09:00 despite high passenger demand.

### Solutions

**Idle Nudge Notifications**
```go
func (s *NudgeService) RunIdleNudges(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        idleDrivers := s.getDriversIdleFor(ctx, 10*time.Minute)
        for _, driverID := range idleDrivers {
            loc := s.getDriverLocation(ctx, driverID)
            zone := s.nearestHighDemandZone(ctx, loc, 5.0)
            if zone != nil && zone.Surge >= 1.2 {
                s.notif.Send(driverID,
                    fmt.Sprintf("Permintaan tinggi di %s (%.1fkm). Surge %.1fx aktif.",
                        zone.Name, zone.Distance, zone.Surge))
            }
        }
    }
}
```

**Zone Demand Display on Driver App**
Show passenger request density as a simple colored overlay on the driver's home screen map. Drivers can see where demand is and self-navigate there.

**Zone Incentive Bonuses (Future)**
Extra Rp 500–1,000 per completed trip for drivers who pick up from underserved zones. Funded from surge revenue.

---

## 5. WebSocket Reliability on Indonesian Mobile Networks

### The Problem
Indonesian mobile networks on motorcycles experience:
- Signal drops in tunnels (Semanggi flyover, Senayan underpass)
- Tower handoffs at high speed (30–50 km/h on motorcycles)
- 3G/4G switching (some Jakarta suburbs still 3G-heavy)
- Typical reconnect needed every 3–10 minutes during a trip

### Solution: Buffered Reconnection

```kotlin
// LocationForegroundService.kt
private val locationBuffer = ArrayDeque<LocationUpdate>(capacity = 20)

// In locationCallback:
if (wsClient.isConnected()) {
    // Flush buffer from previous disconnect
    while (locationBuffer.isNotEmpty()) {
        wsClient.sendLocation(locationBuffer.removeFirst())
    }
    wsClient.sendLocation(currentUpdate)
} else {
    locationBuffer.addLast(currentUpdate)
    if (locationBuffer.size > 20) locationBuffer.removeFirst() // drop oldest
}

// On WebSocket reconnect (onOpen):
// → flush buffer → server gets missing GPS points → trip trace is continuous
```

**Server-side**: the Go hub handles reconnecting drivers gracefully. When a driver reconnects, the existing trip state is read from Redis and no duplicate assignment is created.

---

## 6. Motorcycle-Specific Route Challenges

### The Problem
Jakarta motorcycles use routes invisible to car-centric mapping:
- *Gang* (narrow alleys between housing blocks): not drivable by car
- *Jalan tikus* (rat runs): shortcut roads through neighborhoods
- Contraflow lanes (motorcycles often ignore one-way rules on small roads)
- Footway access (motorcycles park and ride through pedestrian zones)

### Solution: Custom OSRM Motorcycle Profile
See [[03-map-navigation-stack]] for full `motorcycle.lua` profile.

This is a **genuine competitive advantage**: route data specific to how ojek actually rides, not how cars should drive.

---

## 7. Fraud: Fake GPS Positions

### The Problem
Some drivers may use GPS spoofing apps to:
- Appear online in high-demand areas while physically elsewhere
- Rack up fake trip distance (if distance is self-reported)
- Gain dispatch priority by appearing closer to passengers

### Mitigations

**Speed sanity check**:
```go
func isValidGPSUpdate(prev, current LocationUpdate) bool {
    timeDelta := current.Timestamp - prev.Timestamp // seconds
    distance := haversine(prev.LatLng(), current.LatLng()) // km

    if timeDelta <= 0 { return false }

    speedKmh := (distance / float64(timeDelta)) * 3600
    return speedKmh < 200 // > 200km/h on a motorcycle = spoofed
}
```

**Trip distance verification**:
Distance is computed by OSRM from pickup to dropoff coordinates, not self-reported by driver. Driver cannot inflate fare by GPS manipulation.

**Heading consistency check**:
GPS heading should be consistent with direction of movement. Spoofed positions often have random headings.

---

## 8. Concurrent Dispatch Races

### The Problem
Under high load, two dispatch goroutines might simultaneously offer the same driver to two different passengers.

### Solution: Redis SET NX Lock

Already covered in [[04-dispatch-algorithm]]:
```go
SET dispatch:lock:<driver_id> <order_id> NX EX 20
```

`NX` = only set if not exists. Only one goroutine wins. The other gets `false` and moves to the next candidate.

**Also needed**: Ensure that when a driver is on a trip, their status is immediately set to `on_trip` in the Redis hash. This prevents them from appearing in GEOSEARCH results for new orders.

```go
func (h *Hub) onDriverAcceptedTrip(ctx context.Context, driverID, tripID string) {
    h.redis.HSet(ctx, "driver:"+driverID, "status", "on_trip")
    // Remove from available drivers GEO set
    h.redis.ZRem(ctx, "drivers:online", driverID)
    // Add to on-trip set (for operational visibility)
    h.redis.SAdd(ctx, "drivers:on_trip", driverID)
}
```

---

## Summary: Problem → Solution Map

| Problem | Solution | Implementation |
|---------|----------|----------------|
| Jakarta traffic unpredictable | ETA correction factors | [[06-eta-prediction]] |
| GPS drift in CBD | OSRM map matching | [[07-routing-system]] |
| Driver cancellation delay | Immediate re-dispatch + penalty | [[04-dispatch-algorithm]] |
| Driver clustering | Idle nudges + zone display | [[04-dispatch-algorithm]] |
| WebSocket drops on mobile | Buffered reconnect + exponential backoff | [[05-realtime-tracking]] |
| Motorcycle route gaps | Custom motorcycle.lua profile | [[03-map-navigation-stack]] |
| GPS spoofing | Speed sanity check + OSRM distance | this doc |
| Concurrent dispatch race | Redis SET NX offer lock | [[04-dispatch-algorithm]] |
