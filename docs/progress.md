# ADIRD Build Progress

Last updated: 2026-03-17

## VIDI — Go Backend

### Layer 1: Foundation ✅
- [x] Project scaffold (`cmd/server/main.go`, `go.mod`, `internal/config`)
- [x] PostgreSQL 16 + Redis 7 + EMQX 5.7 via docker-compose
- [x] Database migrations (`migrations/000001_initial_schema`)
- [x] Air hot-reload (`make run`)
- [x] Health check endpoint

### Layer 2: Auth ✅
- [x] OTP request + verify (ConsoleSMS for dev)
- [x] JWT HS256 — 15min access + 7-day refresh with rotation
- [x] Redis OTP storage (5-min TTL) + rate limiting (3/10min)
- [x] `FindOrCreateUser` + `FindOrCreateDriver` upserts
- [x] `auth.Middleware` + `RequireRole` middleware
- [x] `/api/v1/me` token verification endpoint

### Layer 3: Driver ✅
- [x] Get/update profile (name, vehicle_type, plate_number)
- [x] Online/offline status toggle
- [x] Redis GEO set `drivers:online` for dispatch proximity search
- [x] Redis HASH `driver:{id}` for metadata
- [x] Incomplete profile guard (blocks going online with PENDING plate)
- [x] FCM token storage

### Layer 4: Order + Dispatch ✅
- [x] Trip creation with fare calculation
- [x] Haversine fallback distance/duration estimate
- [x] OSRM routing client (optional, graceful fallback)
- [x] Async dispatch — GEOSEARCH → distance scoring → MQTT QoS 1 offer
- [x] 15s offer timeout, up to 3 rounds, 5km radius
- [x] Trip state machine: `searching → accepted → en_route → arrived → ongoing → completed/cancelled`
- [x] Passenger cancel order endpoint
- [x] Trip detail endpoint (auth: passenger or assigned driver only)

### Layer 5: Advanced Features ✅
- [x] Surge pricing — Redis demand/supply counters, ~1.1km grid, 2.5× cap
- [x] ETA learning — records `actual / osrm_estimate` to `eta_corrections` post-trip
- [x] Rating system — POST `/{tripID}/rate`, updates driver avg rating in tx
- [x] Trip history — paginated list with driver info
- [x] Driver cancel with cancellation_score penalty

### Fixes Applied ✅
- [x] EMQX auth config fix (remove authentication env vars, use allow-all)
- [x] Driver upsert NOT NULL fix (motor + PENDING placeholders)
- [x] GeoSearchLocation `.Result()` fix
- [x] Trip state machine timestamp column fix (en_route = no timestamp)
- [x] Surge grid key precision fix
- [x] MQTT CleanSession + ResumeSubs fix
- [x] Air PATH fix in Makefile (GOPATH/bin)
- [x] Port 8080 orphan process documentation

### Tooling ✅
- [x] Postman collection (`docs/adird-vidi.postman_collection.json`)
- [x] `.gitignore`
- [x] Initial git commit

---

## VICI — Kotlin Android (Driver App)
- [ ] Project scaffold (Kotlin + Compose + Hilt)
- [ ] OTP login screen
- [ ] Dashboard (online/offline toggle)
- [ ] GPS publishing to MQTT
- [ ] Offer screen (accept/reject)
- [ ] Trip flow screens (en_route → complete)
- [ ] Earnings summary

## VINI — React Dashboard (Ops)
- [ ] Project scaffold
- [ ] Live map with driver positions
- [ ] Trip monitoring table
- [ ] Surge zone heatmap
- [ ] Basic analytics
