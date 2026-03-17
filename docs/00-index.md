---
title: ADIRD Platform — Knowledge Base
tags: [index, odrd, adird]
created: 2026-03-16
---

# ADIRD Platform — Technical Knowledge Base

> **ADIRD** — Online Driver Ride Dispatch (ODRD) platform for Jakarta, Indonesia.
> Solo founder project. Kotlin Android + Go backend + OSRM routing.

---

## Component Naming

| Name | Role | Stack |
|------|------|-------|
| **VICI** | Mobile app (driver + passenger) | Kotlin Android, MapLibre, Paho MQTT |
| **VIDI** | Backend API + broker client | Go modular monolith, EMQX MQTT |
| **VINI** | Web control center (ops) | React + TypeScript + MQTT.js |

*"Veni, Vidi, Vici" — came, saw, conquered.*

---

## 🗺️ Map of Content

### Architecture
- [[01-product-vision]] — Problem, opportunity, competitive strategy
- [[02-system-architecture]] — Core system, modules, architecture diagram
- [[03-map-navigation-stack]] — OSM, OSRM, MapLibre, rerouting

### Design
- [[04-dispatch-algorithm]] — Nearest driver, ETA scoring, offer timeout, surge
- [[05-realtime-tracking]] — GPS, MQTT tracking, EMQX, Redis GEO, Foreground Service
- [[06-eta-prediction]] — Correction model, crowdsourced traffic
- [[07-routing-system]] — OSRM deployment, caching, fallback
- [[08-database-design]] — PostgreSQL DDL, Redis keys, data store decisions
- [[14-mqtt-architecture]] — EMQX setup, topic design, QoS strategy, VICI/VIDI code
- [[15-vini-dashboard]] — Control center web app (React + MQTT.js)
- [[16-scale-50k]] — Infrastructure design for 50K concurrent connections

### Operations
- [[09-infrastructure]] — Hetzner Cloud, Docker Compose, CI/CD, scaling path
- [[10-mvp-features]] — Must-have, nice-to-have, future features

### Strategy
- [[11-hard-problems]] — Dispatch, ETA accuracy, GPS canyons, supply imbalance
- [[12-differentiation]] — Ojek profile, macet crowdsourcing, transparent fare
- [[13-roadmap]] — 6-month solo developer roadmap, milestones

---

## Quick Reference

| Item | Value |
|------|-------|
| Primary market | Jakarta, Indonesia |
| Backend | Go (modular monolith) |
| Mobile | Kotlin Android (native) |
| Maps | OpenStreetMap + OSRM |
| Tile server | OpenFreeMap (free) |
| Database | PostgreSQL 16 + Redis 7 |
| Infra | Hetzner Cloud |
| Budget | < $200/month |
| Team | Solo founder |
| MVP scale | ~100 concurrent drivers |
| Payment | Midtrans (QRIS, GoPay) |
| Notifications | Firebase FCM |

---

## Tech Stack Summary

```
VICI (Kotlin Android)
  ├── MapLibre Android SDK    → OSM tiles from OpenFreeMap
  ├── Eclipse Paho MQTT       → real-time tracking (QoS 0) + offers (QoS 1)
  └── REST (Retrofit/OkHttp)  → trip actions, auth, history

VIDI (Go Modular Monolith)
  ├── Auth        → OTP SMS + JWT (RS256)
  ├── Order       → Trip lifecycle, fare calculation
  ├── Dispatch    → GEO search + ETA scoring + MQTT offer cascade
  ├── MQTT Client → paho.mqtt.golang, subscribes to tracking/offers
  ├── Routing     → OSRM HTTP client + Redis cache
  ├── ETA         → OSRM + learned correction factors
  ├── Notif       → FCM push (backup/non-realtime)
  └── Payment     → Midtrans Snap API

VINI (React + TypeScript)
  ├── MapLibre GL JS    → live driver map (50K dots)
  ├── MQTT.js           → subscribe adird/tracking/driver/+
  └── REST              → driver mgmt, analytics, manual actions

Broker
  └── EMQX 5.x          → 50K concurrent MQTT connections

Data Layer
  ├── PostgreSQL 16  → trips, users, drivers, payments, ratings
  ├── Redis 7        → live locations, dispatch locks, ETA cache
  └── OSRM Server    → Jakarta OSM routing (motorcycle profile)

Infrastructure (Hetzner Cloud, ~$86/mo for 50K scale)
  ├── CX42 (8vCPU/16GB) → VIDI API + Redis + PgBouncer
  ├── CX42 (8vCPU/16GB) → EMQX broker (50K connections)
  ├── CX32 (4vCPU/8GB)  → PostgreSQL (dedicated)
  └── CX22 (2vCPU/4GB)  → OSRM routing server
```

## MQTT Topic Quick Reference

| Topic Pattern | QoS | Direction | Purpose |
|---------------|-----|-----------|---------|
| `adird/tracking/driver/{id}` | 0 | VICI→EMQX→VIDI | GPS location |
| `adird/offer/{driver_id}` | 1 | VIDI→VICI | Ride offer |
| `adird/offer/{driver_id}/response` | 1 | VICI→VIDI | Accept/reject |
| `adird/trip/{trip_id}/location` | 0 | VIDI→VICI | Passenger watches driver |
| `adird/trip/{trip_id}/status` | 1 | VIDI→VICI | Trip state changes |
| `adird/driver/{id}/status` | 1 | VICI→EMQX (LWT) | Online/offline |
| `adird/zone/{zone_id}/surge` | 0 | VIDI→all | Surge multiplier |

---

*Last updated: 2026-03-16*
