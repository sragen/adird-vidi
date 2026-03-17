---
title: Development Roadmap (6 Months, Solo)
tags: [roadmap, planning, milestones]
created: 2026-03-16
---

# Development Roadmap

> **Solo founder. 6 months. First real paying trips.**

> **See also**: [[10-mvp-features]] | [[02-system-architecture]] | [[09-infrastructure]]

---

## Overview

```
Month 1: Foundation     → Infrastructure, auth, project structure
Month 2: Tracking       → GPS, WebSocket, driver dot on map
Month 3: Dispatch       → Orders, fare estimate, dispatch algorithm
Month 4: Navigation     → OSRM route, turn-by-turn, full trip flow
Month 5: Polish         → FCM, ratings, payments, cancellations
Month 6: Beta Launch    → Real drivers, real passengers, first paying trips
```

**Milestone test**: At end of each month, complete the specified end-to-end test. If you can't pass the milestone test, don't move forward — fix what's broken first.

---

## Month 1: Foundation

**Goal**: Working infrastructure. Auth endpoint responds. Android app opens a map.

### Backend Tasks

- [ ] Set up Hetzner CX32 and CX22
- [ ] Docker + Docker Compose installed and running
- [ ] UFW firewall configured (allow 22/80/443 only)
- [ ] GitHub repository created, GitHub Actions deploy pipeline working
- [ ] Go project structure: `cmd/server/main.go`, `internal/` modules
- [ ] Router: `chi` + middleware (logging, recovery, CORS)
- [ ] Configuration: environment variables via `viper`
- [ ] Database migrations: `golang-migrate` setup, `001_initial_schema.sql`
- [ ] Auth module:
  - [ ] Vonage SMS OTP send
  - [ ] OTP verify + JWT generate (RS256)
  - [ ] Token refresh endpoint
- [ ] Health check endpoint: `GET /health` → 200 OK
- [ ] PostgreSQL running in Docker, schema v1 applied

### OSRM Tasks

- [ ] Download Indonesia OSM from Geofabrik
- [ ] Install osmium-tool, filter to Jakarta bbox
- [ ] Write `motorcycle.lua` profile
- [ ] Build OSRM index (osrm-extract → osrm-partition → osrm-customize)
- [ ] Start OSRM server, verify curl query returns valid route
- [ ] Docker Compose for OSRM on CX22

### Android Tasks

- [ ] Android project created (Kotlin, min SDK 26 / Android 8.0)
- [ ] MapLibre SDK added to `build.gradle`
- [ ] Login screen: phone number input + OTP entry
- [ ] Map activity: MapLibre map centered on Jakarta
- [ ] OpenFreeMap tile style configured (no API key needed)
- [ ] API client: Retrofit + OkHttp, JWT handling
- [ ] `strings.xml` internationalization: Indonesian + English

### ✅ Month 1 Milestone Test
```bash
# Test: OTP auth end-to-end
curl -X POST https://api.adird.id/auth/otp/request \
  -d '{"phone": "+6281234567890"}'
# → Receive SMS with OTP code on physical phone
# → Verify OTP → receive JWT access token
# Open Android app → map renders Jakarta
```

---

## Month 2: Driver Tracking

**Goal**: Driver dot visible on passenger map, updating in real time.

### Backend Tasks

- [ ] WebSocket hub: `gorilla/websocket`, register/unregister channels
- [ ] Driver WebSocket handler: `/ws/driver` (JWT auth on connection)
- [ ] GPS message parsing: `{lat, lng, speed, heading, timestamp}`
- [ ] Redis GEOADD on each location update
- [ ] Redis HSET driver metadata hash with 60s TTL
- [ ] Stale driver cleanup goroutine (every 10s)
- [ ] Passenger WebSocket handler: `/ws/passenger/:tripId` (Redis pub/sub subscription)
- [ ] Driver status endpoint: `POST /driver/status` (online/offline)
- [ ] OSRM map matching client: `/match` API for GPS snap-to-road

### Android Tasks (Driver App)

- [ ] Foreground Service: `LocationForegroundService`
- [ ] `AndroidManifest.xml` permissions: FOREGROUND_SERVICE, ACCESS_BACKGROUND_LOCATION
- [ ] FusedLocationProvider: 4s interval moving, 15s idle
- [ ] OkHttp WebSocket client with exponential backoff reconnection
- [ ] GPS buffer (20 items) for disconnect periods
- [ ] Online/offline toggle button on home screen
- [ ] Location permission request flow (runtime permission)

### Android Tasks (Passenger App)

- [ ] WebSocket subscription to driver location
- [ ] Driver marker on MapLibre map (custom icon)
- [ ] Smooth marker animation between GPS updates
- [ ] Show/hide marker based on trip state

### ✅ Month 2 Milestone Test
```
Physical test with 2 Android devices:
Device A (Driver): tap "Go Online"
Device B (Passenger): open map
→ Driver's dot appears on passenger map
→ Drive Device A around — dot moves in real time
→ Close Driver app → dot disappears within 60s
```

---

## Month 3: Dispatch Core

**Goal**: Passenger requests ride → driver receives offer → accepts → trip created.

### Backend Tasks

- [ ] Order service: `POST /order/estimate` (fare estimate, no booking)
- [ ] Order service: `POST /order` (create trip, trigger dispatch)
- [ ] Fare calculation with `fare_configs` table
- [ ] Dispatch engine:
  - [ ] `FindCandidates()`: Redis GEOSEARCH 5km radius
  - [ ] `ScoreAndRank()`: ETA scoring + fairness penalty
  - [ ] `OfferRide()`: Redis NX lock + WebSocket offer + 15s timeout
  - [ ] `Dispatch()`: sequential offer cascade
  - [ ] `retryWithWiderRadius()`: 8km fallback
- [ ] ETA predictor (Phase 1: OSRM raw × 1.4)
- [ ] OSRM ETA client: `/table` endpoint
- [ ] Trip state: `searching → accepted` transition
- [ ] Driver status: set `on_trip` + remove from GEO set on acceptance
- [ ] Offer response handler: `POST /driver/offer/:orderId/respond`

### Android Tasks (Driver App)

- [ ] Offer bottom sheet: pickup zone, dropoff zone, fare estimate
- [ ] 15-second countdown timer (CircularProgressIndicator)
- [ ] Accept/Reject buttons
- [ ] "Offer expired" handling (sheet auto-dismisses)

### Android Tasks (Passenger App)

- [ ] Request ride screen:
  - [ ] GPS auto-detect pickup
  - [ ] MapLibre map pin for pickup adjustment
  - [ ] Destination search (Nominatim autocomplete)
  - [ ] Fare estimate display
  - [ ] Confirm button
- [ ] Searching state: animated "Looking for driver..." screen
- [ ] Driver found: show driver info (name, plate, rating, ETA)

### ✅ Month 3 Milestone Test
```
Physical test: 2 phones + simulator
1. Passenger requests ride (Jl. Sudirman to Jl. Blok M)
2. Fare estimate displayed: ~Rp 24,000
3. Driver receives offer on their device
4. Driver accepts within 15s
5. PostgreSQL: trip record has status='accepted', driver_id set
6. Passenger sees driver info screen
```

---

## Month 4: Navigation and Full Trip Flow

**Goal**: Complete trip from request → cash confirmation, with navigation working.

### Backend Tasks

- [ ] OSRM route client: `/route` with steps + polyline
- [ ] Route endpoint: `GET /order/:id/route`
- [ ] Trip state transitions:
  - [ ] `POST /driver/trip/:id/arrive` → `arrived`
  - [ ] `POST /driver/trip/:id/start` → `ongoing` (record `started_at`)
  - [ ] `POST /driver/trip/:id/complete` → `completed` (record `completed_at`, compute final fare)
- [ ] ETA learner: insert `eta_corrections` record on trip completion
- [ ] Trip location recording: insert GPS points to `trip_locations` table every 5 updates
- [ ] Reroute endpoint: `GET /order/:id/reroute?lat=&lng=`

### Android Tasks (Driver App)

- [ ] Route polyline on MapLibre (dashed blue line)
- [ ] Turn-by-turn instruction strip (OSRM steps, top of screen)
- [ ] "I've Arrived" button (shows when within 100m of pickup)
- [ ] "Start Trip" button (shows after arrived confirmation)
- [ ] "End Trip" button
- [ ] Off-route detection: check deviation every 2 GPS updates
- [ ] Reroute trigger on > 50m deviation

### Android Tasks (Passenger App)

- [ ] Live driver location during trip (WebSocket)
- [ ] Trip progress screen: driver heading to me → en route → arrived
- [ ] Trip in progress screen: live driver position on map
- [ ] Trip completion screen: fare breakdown, cash confirmation

### ✅ Month 4 Milestone Test
```
Full end-to-end trip with 2 physical devices:
1. Passenger requests → driver accepts
2. Driver navigates to pickup (OSRM turn-by-turn working)
3. Driver taps "Arrived" → passenger notified
4. Driver taps "Start Trip"
5. Passenger sees live driver movement on map
6. Driver navigates to destination
7. Driver taps "End Trip"
8. Passenger sees fare: Rp X
9. Driver confirms cash received
10. Trip status in DB: 'completed', timestamps all set
```

---

## Month 5: Polish, Notifications, Payments

**Goal**: All MVP features complete. Ready for beta testers.

### Backend Tasks

- [ ] FCM integration: all trip state transitions push notifications
- [ ] Rating system:
  - [ ] `POST /rating` (submit rating)
  - [ ] Trigger: 48h window after trip completion
  - [ ] Update rolling average on driver/user records
- [ ] Cancellation handling:
  - [ ] Passenger cancel: `POST /order/:id/cancel`
  - [ ] Driver cancel: penalty logic + immediate re-dispatch
  - [ ] System cancel: no drivers available after 2 attempts
- [ ] Midtrans QRIS integration:
  - [ ] `POST /payment/:tripId/create` → generate payment link
  - [ ] Midtrans webhook handler: `POST /payment/webhook`
- [ ] Trip history endpoints:
  - [ ] `GET /order/history?page=1&limit=20` (passenger)
  - [ ] `GET /driver/trips?page=1&limit=20` (driver)
- [ ] Driver earnings endpoint: `GET /driver/earnings?period=today|week`
- [ ] Admin endpoint: `GET /admin/live` (live driver map data, simple JSON)

### Android Tasks

- [ ] FCM receiver: handle all notification types
- [ ] Post-trip rating modal (both apps)
- [ ] Trip history screen (paginated list)
- [ ] Driver earnings screen
- [ ] QRIS payment flow (Midtrans Snap SDK or WebView)
- [ ] Cancellation confirmation dialogs
- [ ] Connection status indicator (offline/online indicator in nav bar)

### ✅ Month 5 Milestone Test
```
Complete test suite with 3 users (1 driver, 2 passengers):
- Complete 5 trips with cash payment
- Complete 2 trips with QRIS payment (verify Midtrans webhook)
- Test driver cancellation → re-dispatch to next driver
- Test passenger cancellation during 'searching'
- Post-trip ratings submitted by both parties
- FCM notifications received on all state changes
- Trip history shows correct data
```

---

## Month 6: Beta Launch

**Goal**: 10 real paying trips with real drivers and passengers in Jakarta.

### Pre-Launch (Week 1–2)

- [ ] Recruit 20–30 driver beta testers
  - Join Jakarta ojek driver WhatsApp groups
  - Post: "Platform baru, fee hanya 10%. Coba gratis 2 minggu pertama"
  - Offer Rp 100,000 bonus for first 10 completed trips
- [ ] Internal testing: 5 friends/family as passenger testers
- [ ] GPS accuracy audit in Sudirman CBD (verify map matching works)
- [ ] Load test with k6: 100 concurrent WebSocket connections, 30-minute session
- [ ] Verify PostgreSQL backup works (test restore)
- [ ] Set up Grafana Cloud dashboard (API latency, WS connections, dispatch rate)
- [ ] Set up BetterStack uptime monitoring (SMS alert when /health fails)

### Beta Operations (Week 3–4)

- [ ] Open to 1 zone: Menteng / Sudirman corridor
- [ ] Monitor dispatch success rate hourly (target: > 80%)
- [ ] Monitor ETA accuracy (compare estimate vs actual in `eta_corrections`)
- [ ] Fix GPS and navigation bugs discovered by drivers
- [ ] Weekly WhatsApp check-in with beta drivers (what's broken?)
- [ ] First driver payout: verify earnings calculation is correct

### ✅ Month 6 Final Milestone
```
Success criteria:
✅ 10 real completed trips with real payment (cash or QRIS)
✅ 5+ distinct passenger users
✅ 10+ active beta drivers
✅ 0 critical bugs in 72-hour window
✅ Dispatch success rate > 80%
✅ No driver GPS "floating" complaints
✅ OSRM turn-by-turn navigation rated "usable" by drivers
```

---

## Post-Launch Priorities (Month 7+)

| Priority | Task | Trigger |
|----------|------|---------|
| 1 | Fix all driver-reported navigation bugs | Ongoing |
| 2 | Ship trip history and earnings screen | v1.1 |
| 3 | Midtrans QRIS fully working | v1.1 |
| 4 | Expand to 3 zones (add Kemang, Blok M) | After 50 active drivers |
| 5 | KTP Dukcapil verification | Before public launch |
| 6 | Multiple vehicle types (car) | After 100 active drivers |
| 7 | iOS app | After proving Android traction |
| 8 | Corporate accounts | After 500 active drivers |

---

## Time Budget (Solo Developer Reality)

You have approximately 8–10 productive engineering hours per day as a solo founder. Some will be lost to driver support, operational issues, and meetings.

| Month | Backend | Android | Ops/Admin | Recruiting |
|-------|---------|---------|-----------|-----------|
| 1 | 60% | 30% | 10% | 0% |
| 2 | 40% | 50% | 10% | 0% |
| 3 | 50% | 40% | 10% | 0% |
| 4 | 40% | 50% | 10% | 0% |
| 5 | 30% | 50% | 10% | 10% |
| 6 | 20% | 20% | 20% | 40% |

**Month 6 is not primarily an engineering month.** It is a recruitment, relationship-building, and bug-fixing month. The majority of your time will be spent talking to drivers and passengers.

---

*This roadmap assumes: Kotlin/Android as primary skill, Go backend as secondary, no prior ops experience (hence managed tools where possible).*
