---
title: MVP Features
tags: [mvp, features, product]
created: 2026-03-16
---

# MVP Features

> **See also**: [[01-product-vision]] | [[13-roadmap]] | [[02-system-architecture]]

---

## Feature Tiers

```
Must Have ──► Launch blockers (platform doesn't work without these)
Nice to Have ► v1.1 (ship within 30 days of launch)
Future ──────► v2.0+ (only when traction is proven)
```

---

## ✅ Must Have (Launch Blockers)

### 1. Authentication
- Phone number login (Indonesian format: `+628xx`)
- OTP via SMS (Vonage, 6-digit, 5-minute expiry)
- JWT sessions: 15-minute access token, 30-day refresh
- Separate flows: passenger registration vs driver registration

### 2. Trip Request (Passenger)
- Auto-detect current location (GPS + reverse geocode via Nominatim)
- Map pin for pickup location (MapLibre + Nominatim search)
- Destination input with autocomplete (Nominatim search)
- Fare estimate displayed before confirmation (OSRM distance + rate table)
- One-tap "Request Ride" confirmation

### 3. Dispatch System
- Nearest available driver search (Redis GEO, 5km radius)
- ETA-based scoring (OSRM + 1.4x Jakarta correction)
- 15-second offer timeout per driver
- Sequential fallback (try next candidate if timeout/reject)
- Re-dispatch on driver cancellation (immediate)

### 4. Trip State Machine

```
                  ┌──────────┐
                  │ searching │ ◄── Passenger requests ride
                  └────┬─────┘
                       │ Driver accepts
                  ┌────▼─────┐
                  │ accepted  │
                  └────┬─────┘
                       │ Driver starts navigation
                  ┌────▼─────┐
                  │ en_route  │ ◄── Driver heading to pickup
                  └────┬─────┘
                       │ Driver taps "Arrived"
                  ┌────▼─────┐
                  │  arrived  │ ◄── Driver at pickup
                  └────┬─────┘
                       │ Driver taps "Start Trip"
                  ┌────▼─────┐
                  │  ongoing  │ ◄── Trip in progress
                  └────┬─────┘
                       │ Driver taps "End Trip"
                  ┌────▼──────┐
                  │ completed  │
                  └───────────┘

At any point before "ongoing":
searching/accepted/en_route/arrived ──► cancelled
```

### 5. Real-Time Driver Location
- Driver dot visible on passenger map as soon as accepted
- Live updates every 4 seconds via WebSocket
- Map-matched GPS to avoid "floating in building" artifact
- Smooth animation between GPS updates (interpolation in MapLibre)

### 6. Driver Navigation
- OSRM route polyline overlaid on driver map
- Turn-by-turn instruction strip at top of screen (OSRM steps)
- Rerouting when deviation > 50m (new OSRM route request)
- Bearing-up camera (map rotates to driver's heading)

### 7. Fare Calculation
```
fare = base_fare + (distance_km × per_km_rate) + (duration_min × per_min_rate)
     = max(fare, min_fare)
     × surge_multiplier
     → rounded to nearest Rp 500
```

Rates stored in `fare_configs` table (editable without redeploy).

### 8. Cash Payment Only
- No payment integration for MVP
- Driver confirms "cash received" after trip completion
- Passenger sees final fare displayed on completion screen
- Trip marked paid immediately on cash confirmation

### 9. Push Notifications (FCM)
All trip state transitions trigger FCM push:

| Event | Recipient | Message |
|-------|-----------|---------|
| New offer | Driver | "Ada penumpang baru! Terima dalam 15 detik" |
| Driver accepted | Passenger | "Driver ditemukan! {name} sedang menuju kamu" |
| Driver arrived | Passenger | "{name} sudah sampai di lokasi penjemputan" |
| Trip started | Passenger | "Perjalanan dimulai. Sampai tujuan aman!" |
| Trip completed | Both | "Perjalanan selesai. Total: Rp {fare}" |
| No driver found | Passenger | "Tidak ada driver tersedia. Coba lagi?" |

### 10. Post-Trip Ratings
- 1–5 star modal shown to both driver and passenger after completion
- Optional short comment (max 150 chars)
- 48-hour window (after that, rating is skipped)
- Rating stored, rolling average updated on driver/user records
- Minimum 1 rating required before driver score affects dispatch priority

---

## 🟡 Nice to Have (v1.1 — Within 30 Days)

### Midtrans QRIS Payment
- Generate QRIS payment link after trip completion
- Passenger scans QR code with any e-wallet (GoPay, OVO, Dana, QRIS-enabled banks)
- Midtrans webhook confirms payment to backend
- Trip marked paid, driver notified

### Trip History
- Last 20 trips for passenger (paginated)
- Last 20 trips for driver with earnings breakdown
- Filter by date range
- Trip receipt detail (fare breakdown, route map thumbnail)

### Driver Earnings Dashboard
- Today's earnings vs yesterday
- This week vs last week
- Number of trips, acceptance rate
- Cancellation rate (with threshold warning)

### Cancellation Penalty Display
- Show driver their current cancellation score
- Alert when approaching review threshold
- Transparent policy display

---

## 🔵 Future (v2.0+)

### Multiple Vehicle Types
- Motorcycle (`motor`) — default, Rp 2,000/km
- Car (`car`) — premium, Rp 4,000/km
- Separate fare tables per type
- Passenger selects vehicle type on request screen

### Surge Pricing UI
- Visual indicator on passenger map (zone color overlay)
- Surge multiplier displayed before fare confirmation
- "Wait X minutes for normal price" suggestion when surge > 1.5x

### Scheduled Rides
- Book ojek for specific future time
- Pre-dispatch 10 minutes before scheduled time
- Useful for airport pickups, scheduled meetings

### Corporate Accounts (B2B)
- Company creates account, adds employee phones
- Monthly billing instead of per-trip payment
- Trip approval workflow (optional)
- Spend analytics dashboard for finance team

### iOS App
- Jakarta Android market is 85–90% of smartphone users
- iOS app after Android traction is proven
- Share 90% of business logic via API — only UI layer differs

### Driver Supply Heatmap (Ops Tool)
- Internal web dashboard showing live driver density
- Demand heatmap by zone
- Zone-based nudge trigger controls
- Used by ops team to manually boost supply in underserved zones

### Crowdsourced Traffic Layer
- Driver GPS speed data → real-time macet detection
- Display traffic overlay on passenger map
- Route around detected congestion in OSRM
- See [[06-eta-prediction]] Phase 3 for implementation

### Offline Map Support
- Pre-download Jakarta zone tiles on WiFi
- Navigation works in tunnels and poor signal areas
- MapLibre tile region download API
- ~200MB per Jakarta zone

---

## MVP Launch Checklist

Before opening to public:

- [ ] 20+ recruited driver beta testers
- [ ] 5+ internal passenger testers (friends/family)
- [ ] All Must Have features working end-to-end
- [ ] WebSocket stability tested (100 concurrent connections, no drops in 30-min window)
- [ ] OSRM routing tested for 10 Jakarta zones
- [ ] Fare calculation verified manually (5 test trips)
- [ ] FCM notifications delivered on all state transitions
- [ ] GPS accuracy acceptable in Sudirman CBD (map matching applied)
- [ ] Dispatch success rate > 80% in test area
- [ ] Cancellation re-dispatch working correctly
- [ ] PostgreSQL backup running and verified restorable
- [ ] Nginx rate limiting tested (OTP endpoint especially)
- [ ] SSL certificate installed and auto-renewing

---

*See [[13-roadmap]] for when each feature tier is scheduled.*
