---
title: Core System Architecture
tags: [architecture, backend, go, system-design]
created: 2026-03-16
---

# Core System Architecture

> **See also**: [[00-index]] | [[05-realtime-tracking]] | [[09-infrastructure]]

---

## Component Names

| Name | Role |
|------|------|
| **VICI** | Mobile app — Kotlin Android (driver + passenger) |
| **VIDI** | Backend — Go modular monolith |
| **VINI** | Web control center — React + TypeScript |

---

## Architecture Diagram

```
┌──────────────────────────────────────────────────────────────┐
│                          VICI                                 │
│               (Kotlin Android — driver + passenger)           │
├──────────────────────────┬───────────────────────────────────┤
│   Passenger App          │   Driver App                      │
│   MapLibre SDK           │   MapLibre SDK + ForegroundService│
│   Paho MQTT (subscribe)  │   Paho MQTT (publish GPS QoS 0)  │
│   REST API (actions)     │   REST API (trip actions)        │
└──────┬───────────────────┴──────────────────┬────────────────┘
       │  MQTT (SSL :8883)                     │  MQTT (SSL :8883)
       │  REST (HTTPS :443)                    │  REST (HTTPS :443)
       ▼                                       ▼
┌─────────────────────────────────────────────────────────────┐
│                      EMQX Broker                             │
│                 (50K concurrent connections)                  │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Rule Engine: adird/tracking/driver/+ → VIDI webhook  │   │
│  │  ACL: driver can only pub/sub own topics              │   │
│  │  JWT Auth: validates token on connect                 │   │
│  │  LWT: auto-publish offline status on disconnect       │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────────────┬──────────────────────────────┘
                               │  MQTT (internal :1883)
                               │  WebHook (HTTP)
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    VIDI — Go Modular Monolith                 │
│  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐   │
│  │  Auth   │ │  Order   │ │ Dispatch │ │  MQTT Client │   │
│  │ /auth/* │ │ /order/* │ │  Engine  │ │  paho.mqtt   │   │
│  └─────────┘ └──────────┘ └──────────┘ └──────────────┘   │
│  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌──────────────┐   │
│  │ Routing │ │   ETA    │ │  Notif   │ │   Payment    │   │
│  │  OSRM   │ │  Module  │ │  FCM     │ │  Midtrans    │   │
│  └─────────┘ └──────────┘ └──────────┘ └──────────────┘   │
└────────────────────────┬─────────────────────────────────────┘
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
  ┌──────────────┐ ┌──────────┐ ┌──────────────┐
  │  PostgreSQL  │ │  Redis   │ │  OSRM Server │
  │  + PgBouncer │ │          │ │  (Hetzner)   │
  └──────────────┘ └──────────┘ └──────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    VINI — Web Control Center                  │
│               (React + TypeScript + MapLibre GL JS)           │
│  ├── MQTT.js → subscribes adird/tracking/driver/+ (QoS 0)   │
│  ├── MQTT.js → subscribes adird/zone/+/surge (QoS 0)        │
│  └── REST API → driver mgmt, analytics, manual actions       │
└─────────────────────────────────────────────────────────────┘
```

## Real-Time Communication: MQTT vs REST

| Use Case | Protocol | Reason |
|----------|----------|--------|
| Driver GPS tracking | **MQTT QoS 0** | High-frequency, loss-tolerant |
| Ride offer to driver | **MQTT QoS 1** | Guaranteed delivery required |
| Offer accept/reject | **MQTT QoS 1** | Critical response |
| Trip status updates | **MQTT QoS 1** | Retained, passenger receives on subscribe |
| Driver location to passenger | **MQTT QoS 0** | High-frequency, interpolated |
| Zone surge broadcast | **MQTT QoS 0** | Informational, periodic |
| Trip creation / fare estimate | **REST** | Request-response |
| Driver arrive / start / end | **REST** | Idempotent action |
| Trip history | **REST** | Paginated query |
| Auth / OTP | **REST** | Standard HTTP |
| Payments | **REST** | Webhook-based |

---

## Module Responsibilities

### Auth (`/auth/*`)
- OTP generation and verification via SMS (Vonage)
- JWT issuance: access token 15min, refresh token 30 days
- Token refresh endpoint
- Driver / passenger registration
- Sessions stored in Redis; user records in PostgreSQL

### Order (`/order/*`)
- Trip creation and fare estimation
- Trip state machine transitions
- Trip history queries
- Source of truth for all trip lifecycle events
- Writes to PostgreSQL; maintains hot trip state in Redis JSON

### Dispatch Engine (internal goroutine pool)
- Triggered by new order events
- Executes: candidate search → ETA scoring → offer cascade
- Most complex internal module
- See [[04-dispatch-algorithm]] for full design

### Tracking (`/ws/driver`, `/ws/passenger`)
- WebSocket hub (gorilla/websocket)
- GPS ingestion from driver apps
- Broadcasts location to subscribed passengers
- Feeds Redis GEO index for dispatch
- See [[05-realtime-tracking]] for full design

### Routing (internal OSRM client)
- HTTP client to OSRM server with Redis caching
- Exposes `Route(origin, dest)` and `ETA(origin, dest)` interfaces
- See [[07-routing-system]] for full design

### ETA Module
- Wraps Routing module
- Applies learned correction factors from `eta_corrections` table
- Returns corrected ETA to Order and Dispatch modules
- See [[06-eta-prediction]] for full design

### Notif (FCM)
- Firebase Cloud Messaging wrapper
- Typed push payloads: offer, trip state, arrival

### Payment (Midtrans)
- Midtrans Snap API client
- Generates QRIS payment links
- Handles webhook callbacks
- Updates payment status in PostgreSQL

---

## Project Structure

```
adird/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── auth/
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── repository.go
│   ├── order/
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── repository.go
│   ├── dispatch/
│   │   ├── engine.go
│   │   ├── scorer.go
│   │   └── offer.go
│   ├── tracking/
│   │   ├── hub.go
│   │   └── handler.go
│   ├── routing/
│   │   ├── osrm_client.go
│   │   └── cache.go
│   ├── eta/
│   │   ├── predictor.go
│   │   └── learner.go
│   ├── notif/
│   │   └── fcm_client.go
│   ├── payment/
│   │   ├── midtrans_client.go
│   │   └── webhook.go
│   └── shared/
│       ├── models.go
│       ├── errors.go
│       └── geo.go
├── migrations/
│   ├── 001_initial_schema.sql
│   └── 002_eta_corrections.sql
├── docker-compose.yml
├── Dockerfile
└── .github/
    └── workflows/
        └── deploy.yml
```

---

## Why Modular Monolith, Not Microservices

Microservices require:
- Distributed tracing infrastructure
- Service mesh (Istio/Linkerd)
- Inter-service authentication
- Separate CI/CD pipelines per service
- Operational overhead: **~40% of solo dev time on infrastructure**

A modular monolith with clear `internal/` package boundaries gives the same code organization benefit **without the operational cost**.

**Key discipline**: modules communicate through Go interfaces, never by importing each other's internals directly.

```go
// ✅ Correct: depend on interface
type Router interface {
    Route(ctx context.Context, origin, dest LatLng) (*RouteResult, error)
}

// ❌ Wrong: import other module's internals directly
import "adird/internal/routing" // dispatch should not do this
```

### Extraction Path
When you need to extract a service later (e.g., dispatch engine at 1000+ drivers), the module boundary makes extraction straightforward. The interface contract becomes the gRPC/HTTP contract.

---

## API Endpoints Reference

### Auth
| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/otp/request` | Request OTP SMS |
| POST | `/auth/otp/verify` | Verify OTP, return JWT |
| POST | `/auth/token/refresh` | Refresh access token |

### Order
| Method | Path | Description |
|--------|------|-------------|
| POST | `/order/estimate` | Fare estimate (no booking) |
| POST | `/order` | Create trip / request ride |
| GET | `/order/:id` | Get trip details |
| POST | `/order/:id/cancel` | Cancel trip |
| GET | `/order/history` | Trip history (paginated) |

### Driver
| Method | Path | Description |
|--------|------|-------------|
| POST | `/driver/status` | Set online/offline |
| POST | `/driver/offer/:order_id/respond` | Accept/reject offer |
| POST | `/driver/trip/:id/arrive` | Mark arrived at pickup |
| POST | `/driver/trip/:id/start` | Start trip |
| POST | `/driver/trip/:id/complete` | Complete trip |

### WebSocket
| Path | Direction | Description |
|------|-----------|-------------|
| `/ws/driver` | Driver → Server | GPS location stream |
| `/ws/passenger/:trip_id` | Server → Passenger | Driver location updates |

---

*See [[08-database-design]] for data models, [[09-infrastructure]] for deployment.*
