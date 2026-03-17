# ADIRD Build Progress

Last updated: 2026-03-17 (session 3)

## VIDI — Go Backend

### Layer 1: Foundation ✅
- [x] Project scaffold (`cmd/server/main.go`, `go.mod`, `internal/config`)
- [x] PostgreSQL 16 + Redis 7 + EMQX 5.7 via docker-compose
- [x] Database migrations (`migrations/000001_initial_schema`)
- [x] Air hot-reload (`make run`) — auto-kills :8080 on start
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
- [x] MQTT CleanSession + ResumeSubs + goroutine fix
- [x] Air PATH fix in Makefile (GOPATH/bin)
- [x] Port 8080 auto-kill baked into `make run`
- [x] OTP GetDel → Get+conditional Del (retries now work)
- [x] PENDING plate unique violation → md5(phone)[0:8] unique placeholder per driver
- [x] Admin auth — new `admins` table (migration 000002), seeded admin, `role: 'admin'` in JWT

### Tooling ✅
- [x] Postman collection (`docs/adird-vidi.postman_collection.json`)
- [x] `.gitignore`
- [x] Git pushed to https://github.com/sragen/adird-vidi.git
- [x] Progress checklist (`docs/progress.md`)

---

## VINI — React Ops Dashboard

### Layer 1: Scaffold + Core Pages ✅
- [x] Vite 8 + React 18 + TypeScript + Tailwind CSS v4
- [x] Zustand stores (driversStore, zonesStore, alertsStore)
- [x] MQTT.js client (ws://localhost:8083/mqtt, auto-reconnect)
- [x] Login page — OTP flow, JWT saved to localStorage
- [x] Sidebar navigation (4 pages)
- [x] Live map — MapLibre GL, MQTT driver dots (green/amber/red), Jakarta center
- [x] Drivers page — table with status/rating/trips/cancel%
- [x] Trips page — paginated history with fare/distance/status badges
- [x] Analytics page — KPI cards, daily trips bar chart, revenue line chart
- [x] Vite proxy `/api` → `http://localhost:8080`
- [x] Build verified clean (tsc + vite build)

### Layer 2: Enhancements (next)
- [x] Admin endpoints in VIDI (`/admin/drivers`, `/admin/trips`) — RequireRole("admin")
- [x] VINI Drivers + Trips pages now call admin endpoints (all data, not caller-scoped)
- [x] VINI login uses `role: 'admin'` — separate from driver/passenger
- [ ] Driver detail drawer on map click
- [ ] Real-time dispatch monitor (poll every 5s)
- [ ] Surge zone overlay on map
- [ ] Trip detail modal with GPS trace

---

## VICI — Kotlin Android (Driver App)
- [ ] Project scaffold (Kotlin + Compose + Hilt)
- [ ] OTP login screen
- [ ] Dashboard (online/offline toggle)
- [ ] GPS publishing to MQTT
- [ ] Offer screen (accept/reject)
- [ ] Trip flow screens (en_route → complete)
- [ ] Earnings summary

---

## Dev Tools Installed
- [x] OrbStack (Docker alternative for Apple Silicon)
- [x] golang-migrate
- [x] air (Go hot-reload) — ~/go/bin/air
- [x] gh CLI v2.88.1
- [x] Node 22 — /usr/local/opt/node@22/bin
- [x] RedisInsight 3.2.0 — /Applications
