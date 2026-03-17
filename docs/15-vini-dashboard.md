---
title: VINI — Web Control Center Dashboard
tags: [vini, dashboard, react, mqtt, frontend, operations]
created: 2026-03-16
---

# VINI — Web Control Center Dashboard

VINI is the web-based control center for the ADIRD operations team. It provides real-time visibility into the entire platform: live driver positions, dispatch activity, zone demand, trip status, and operational analytics. VINI does not handle passenger-facing or driver-facing features — those are VICI's domain.

Related: [[14-mqtt-architecture]] | [[02-system-architecture]] | [[04-dispatch-algorithm]] | [[16-scale-50k]]

---

## 1. Overview

VINI connects to two data sources:

- **MQTT** (via EMQX WebSocket port 8084): live driver positions, zone surge, system alerts — all real-time, subscribe-only
- **REST API** (VIDI): everything else — driver records, trip history, analytics aggregates, manual actions

VINI has the `vini` role in the EMQX ACL, which grants subscribe access to all topics and no publish rights. See [[14-mqtt-architecture#3.2]] for ACL details.

---

## 2. Technology Stack

| Layer | Choice | Rationale |
|---|---|---|
| Framework | React 18 + TypeScript | Typed, component-based, large ecosystem |
| Map | MapLibre GL JS | Same OSM tile provider as VICI, consistent visual language |
| Real-time | MQTT.js (WebSocket) | Browser MQTT over EMQX port 8084, same broker |
| State | Zustand | Lightweight, no boilerplate; suitable for high-frequency map updates |
| Charts | Recharts | Composable, TypeScript-native |
| UI Components | Tailwind CSS + shadcn/ui | Utility-first styling, accessible component primitives |
| Build | Vite | Fast HMR, TypeScript out of the box |
| HTTP Client | ky | Minimal fetch wrapper, typed |

MapLibre GL JS is chosen over Google Maps or Mapbox for the same reason as VICI: no per-tile billing, uses OpenFreeMap tiles, consistent coordinate rendering with the driver app.

---

## 3. Key Screens

### 3.1 Live Operations Map

The primary operational view. Renders all online drivers as animated dots overlaid on Jakarta.

**Data sources:**
- Driver positions: MQTT subscription `adird/tracking/driver/+` QoS 0
- Zone surge overlay: MQTT subscription `adird/zone/+/surge` QoS 0 (retained)
- Active trip polylines: REST GET `/api/v1/trips/active` polled every 15s

**Driver dot color coding:**
- Green (`#22c55e`): available, GPS updated within last 30s
- Yellow (`#f59e0b`): on_trip
- Red (`#ef4444`): stale — last GPS update >5 minutes ago (LWT fired or app backgrounded)

**Interactions:**
- Click driver dot → right drawer opens with: name, plate, phone (masked), current trip ID, today's completed trips, today's gross earnings, rating, online duration
- Zone layer toggle: overlay demand heatmap or surge multipliers per zone
- Filter controls: zone selector, status filter (available / on_trip / stale), vehicle type

**Performance note:** At 30K online drivers updating every 4s, the map store receives ~7,500 updates/second. Zustand batch-updates are used to coalesce updates before triggering React re-renders. Map markers use a WebGL layer (MapLibre symbol layer), not DOM elements — rendering 30K DOM nodes would be unacceptable.

### 3.2 Dispatch Monitor

Live feed of all dispatch events. Used by ops team to identify matching failures.

**Data sources:**
- Dispatch events: REST GET `/api/v1/dispatch/events` polled every 5s
- (Future: MQTT `adird/system/dispatch` topic for sub-second latency)

**Table columns:** order_id, passenger pickup zone, dispatch attempts, first offer sent at, time-to-accept (seconds), assigned driver ID

**Visual rules:**
- Orders with >2 dispatch attempts: row highlighted red
- Orders that timed out with no assignment: persistent red row until manually dismissed

**Metrics header:**
- Dispatch success rate (last 1h): gauge chart 0–100%
- Average time-to-match (last 1h): seconds, vs 30-day baseline
- Current pending orders: count badge

### 3.3 Zone Management

Manual override interface for Jakarta demand zones.

**Displayed per zone:**
- Zone name and boundary polygon
- Current surge multiplier (from retained MQTT `adird/zone/{id}/surge`)
- Live driver count vs active order count (bar chart, updated via REST)
- Demand level label: low / normal / high / critical

**Actions:**
- Surge multiplier override: slider 1.0–3.0 → POST `/api/v1/admin/zones/{zone_id}/surge`
- Send nudge to idle drivers in zone: one-click → POST `/api/v1/admin/zones/{zone_id}/nudge`
  - Backend sends FCM push + MQTT message to all available drivers in zone with an incentive display

### 3.4 Driver Management

Full driver account management.

**List view columns:** status badge, name, rating (1 decimal), trips today, earnings today (IDR), cancellation rate (%), registered date

**Actions per driver:**
- View trip history: opens paginated trip list with fare breakdown and GPS trace
- Approve / reject registration: for pending driver applications with document upload review
- Suspend account: sets driver status to `suspended`, blocks dispatch and MQTT connect
- Unsuspend: restores access

**Bulk actions:** export CSV, bulk approve

### 3.5 Trip Management

Active and historical trip view with manual intervention capability.

**Active trips table:** trip_id, passenger zone, pickup address, driver name, status badge, elapsed time, current fare estimate

**Trip detail view:**
- GPS trace polyline on MapLibre map (driver path from pickup to current or dropoff)
- Fare breakdown: base fare, distance fare, surge multiplier, platform fee, driver payout
- Status timeline: accepted → en_route → arrived → ongoing → completed (with timestamps)
- Passenger contact: masked phone number

**Manual intervention actions** (ops admin only):
- Force-complete trip: for cases where driver and passenger dispute completion
- Trigger refund: POST to Midtrans refund endpoint via VIDI
- Cancel and reassign: cancels current trip, places order back into dispatch queue

### 3.6 Analytics Dashboard

Aggregated platform health. Loaded via REST on page navigation, not real-time.

**Charts:**
- Daily/weekly trip volume: line chart, last 30 days, with 7-day moving average
- Revenue breakdown: stacked bar chart — total fare / driver payout / platform fee / promotions
- ETA accuracy: scatter plot of estimated vs actual trip duration, % within ±2 minutes
- Zone performance heatmap: trips per zone per hour (color matrix)
- Driver retention: new vs returning drivers per week (stacked bar)
- GPS update rate per hour: sanity check for tracking pipeline health

**Time range selector:** 24h / 7d / 30d / custom range

---

## 4. MQTT.js Connection (Browser)

VINI connects to EMQX over WebSocket TLS (port 8084). EMQX handles the MQTT-over-WebSocket upgrade.

```typescript
// src/lib/mqtt.ts
import mqtt, { MqttClient } from 'mqtt'

export function createVINIClient(token: string): MqttClient {
  const client = mqtt.connect('wss://mqtt.adird.id:8084/mqtt', {
    clientId: `vini_admin_${Date.now()}_${Math.random().toString(36).slice(2)}`,
    username: 'vini',
    password: token,         // JWT from VINI admin login
    clean: true,             // no persistent session needed for dashboard
    reconnectPeriod: 3000,   // attempt reconnect every 3s
    connectTimeout: 10000,
    keepalive: 60,
  })

  client.on('connect', () => {
    console.log('[VINI MQTT] connected to EMQX')

    // All driver GPS — primary data source for live map
    client.subscribe('adird/tracking/driver/+', { qos: 0 })

    // Zone surge overlays (retained: get current state on connect)
    client.subscribe('adird/zone/+/surge', { qos: 0 })

    // System-level operational alerts
    client.subscribe('adird/system/alerts', { qos: 1 })
  })

  client.on('error', (err) => {
    console.error('[VINI MQTT] error:', err.message)
  })

  client.on('offline', () => {
    console.warn('[VINI MQTT] offline, reconnecting...')
  })

  return client
}
```

Message routing — wire MQTT messages to Zustand stores:

```typescript
// src/lib/mqttRouter.ts
import type { MqttClient } from 'mqtt'
import { useDriversStore } from '@/stores/driversStore'
import { useZonesStore } from '@/stores/zonesStore'
import { useAlertsStore } from '@/stores/alertsStore'

export function wireMessageRouter(client: MqttClient) {
  client.on('message', (topic: string, payload: Buffer) => {
    const parts = topic.split('/')

    // adird/tracking/driver/{driver_id}
    if (parts[1] === 'tracking' && parts[2] === 'driver' && parts[3]) {
      const driverId = parts[3]
      try {
        const data = JSON.parse(payload.toString())
        useDriversStore.getState().updateDriver(driverId, data)
      } catch {
        // malformed payload — discard
      }
      return
    }

    // adird/zone/{zone_id}/surge
    if (parts[1] === 'zone' && parts[3] === 'surge' && parts[2]) {
      const zoneId = parts[2]
      try {
        const data = JSON.parse(payload.toString())
        useZonesStore.getState().updateZone(zoneId, data)
      } catch {}
      return
    }

    // adird/system/alerts
    if (parts[1] === 'system' && parts[2] === 'alerts') {
      try {
        const alert = JSON.parse(payload.toString())
        useAlertsStore.getState().addAlert(alert)
      } catch {}
    }
  })
}
```

---

## 5. Zustand Stores

### 5.1 Driver Location Store

```typescript
// src/stores/driversStore.ts
import { create } from 'zustand'

interface DriverLocation {
  driverId: string
  lat: number
  lng: number
  speed: number
  heading: number
  updatedAt: number
  status: 'available' | 'on_trip' | 'stale'
}

interface DriversStore {
  drivers: Map<string, DriverLocation>
  updateDriver: (driverId: string, data: {
    lat: number
    lng: number
    speed: number
    heading: number
    ts: number
  }) => void
  getOnlineCount: () => number
  getStaledrivers: () => DriverLocation[]
}

export const useDriversStore = create<DriversStore>((set, get) => ({
  drivers: new Map(),

  updateDriver: (driverId, data) => {
    set(state => {
      const drivers = new Map(state.drivers)
      const nowSec = Date.now() / 1000
      // Stale threshold: no update in 300s (5 minutes)
      const status: DriverLocation['status'] =
        (nowSec - data.ts) > 300 ? 'stale' : 'available'

      drivers.set(driverId, {
        driverId,
        lat: data.lat,
        lng: data.lng,
        speed: data.speed,
        heading: data.heading,
        updatedAt: data.ts,
        status,
      })
      return { drivers }
    })
  },

  getOnlineCount: () => {
    const nowSec = Date.now() / 1000
    return Array.from(get().drivers.values())
      .filter(d => (nowSec - d.updatedAt) < 60).length
  },

  getStaledrivers: () => {
    const nowSec = Date.now() / 1000
    return Array.from(get().drivers.values())
      .filter(d => (nowSec - d.updatedAt) > 300)
  },
}))
```

### 5.2 Zone Surge Store

```typescript
// src/stores/zonesStore.ts
import { create } from 'zustand'

interface ZoneSurge {
  zoneId: string
  multiplier: number
  demandLevel: 'low' | 'normal' | 'high' | 'critical'
  updatedAt: number
}

interface ZonesStore {
  zones: Map<string, ZoneSurge>
  updateZone: (zoneId: string, data: {
    zone_id: string
    multiplier: number
    demand_level: string
  }) => void
}

export const useZonesStore = create<ZonesStore>((set) => ({
  zones: new Map(),

  updateZone: (zoneId, data) => {
    set(state => {
      const zones = new Map(state.zones)
      zones.set(zoneId, {
        zoneId,
        multiplier: data.multiplier,
        demandLevel: data.demand_level as ZoneSurge['demandLevel'],
        updatedAt: Date.now() / 1000,
      })
      return { zones }
    })
  },
}))
```

### 5.3 Alerts Store

```typescript
// src/stores/alertsStore.ts
import { create } from 'zustand'

interface SystemAlert {
  id: string
  severity: 'info' | 'warning' | 'critical'
  message: string
  ts: number
  acknowledged: boolean
}

interface AlertsStore {
  alerts: SystemAlert[]
  addAlert: (data: Omit<SystemAlert, 'id' | 'acknowledged'>) => void
  acknowledge: (id: string) => void
  unacknowledgedCount: () => number
}

export const useAlertsStore = create<AlertsStore>((set, get) => ({
  alerts: [],

  addAlert: (data) => {
    set(state => ({
      alerts: [
        { ...data, id: crypto.randomUUID(), acknowledged: false },
        ...state.alerts.slice(0, 99), // keep last 100 alerts
      ]
    }))
  },

  acknowledge: (id) => {
    set(state => ({
      alerts: state.alerts.map(a =>
        a.id === id ? { ...a, acknowledged: true } : a
      )
    }))
  },

  unacknowledgedCount: () =>
    get().alerts.filter(a => !a.acknowledged).length,
}))
```

---

## 6. MapLibre Driver Layer

Driver dots use a MapLibre GL symbol layer backed by a GeoJSON source. The source is updated whenever the Zustand store changes.

```typescript
// src/components/map/DriverLayer.tsx
import { useEffect, useRef } from 'react'
import { useMap } from 'react-map-gl/maplibre'
import { useDriversStore } from '@/stores/driversStore'
import type { FeatureCollection, Point } from 'geojson'

const SOURCE_ID = 'drivers'
const LAYER_ID = 'drivers-layer'

export function DriverLayer() {
  const { current: map } = useMap()
  const drivers = useDriversStore(state => state.drivers)
  const lastUpdate = useRef<number>(0)

  useEffect(() => {
    if (!map) return

    // Throttle to max 10 re-renders per second (coalesce rapid MQTT updates)
    const now = Date.now()
    if (now - lastUpdate.current < 100) return
    lastUpdate.current = now

    const geojson: FeatureCollection<Point> = {
      type: 'FeatureCollection',
      features: Array.from(drivers.values()).map(d => ({
        type: 'Feature',
        geometry: { type: 'Point', coordinates: [d.lng, d.lat] },
        properties: {
          driverId: d.driverId,
          heading: d.heading,
          status: d.status,
        },
      })),
    }

    const source = map.getSource(SOURCE_ID) as maplibregl.GeoJSONSource | undefined
    if (source) {
      source.setData(geojson)
    }
  }, [map, drivers])

  return null // rendering handled by MapLibre layer, not React DOM
}
```

Driver dot color is driven by a MapLibre data-driven paint expression:

```typescript
// Symbol layer paint — color by status property
'circle-color': [
  'match',
  ['get', 'status'],
  'available', '#22c55e',
  'on_trip',   '#f59e0b',
  'stale',     '#ef4444',
  '#94a3b8'  // default: unknown
]
```

---

## 7. REST API Usage

VINI uses REST for all non-real-time operations. MQTT is not used for writes.

| Operation | Method | Endpoint | Notes |
|---|---|---|---|
| Admin login | POST | `/api/v1/auth/admin/login` | Returns JWT |
| List drivers | GET | `/api/v1/admin/drivers` | Paginated, filterable |
| Driver detail | GET | `/api/v1/admin/drivers/{id}` | Profile + current state |
| Approve driver | POST | `/api/v1/admin/drivers/{id}/approve` | |
| Suspend driver | POST | `/api/v1/admin/drivers/{id}/suspend` | |
| List trips | GET | `/api/v1/admin/trips` | Paginated, filterable by status/date |
| Trip detail | GET | `/api/v1/admin/trips/{id}` | Includes GPS trace |
| Force complete trip | POST | `/api/v1/admin/trips/{id}/complete` | Admin override |
| Trigger refund | POST | `/api/v1/admin/trips/{id}/refund` | Midtrans passthrough |
| Zone list | GET | `/api/v1/admin/zones` | |
| Override surge | POST | `/api/v1/admin/zones/{id}/surge` | Publishes to MQTT internally |
| Send zone nudge | POST | `/api/v1/admin/zones/{id}/nudge` | FCM + MQTT nudge to idle drivers |
| Analytics summary | GET | `/api/v1/admin/analytics/summary` | Aggregated metrics |
| Dispatch events | GET | `/api/v1/admin/dispatch/events` | Polled every 5s |

VINI passes the admin JWT as `Authorization: Bearer {token}` on all requests. VIDI validates the `role: "vini"` claim.

---

## 8. Authentication Flow

VINI admin login is separate from driver/passenger auth. Admin accounts are stored in PostgreSQL with bcrypt passwords. There is no self-registration — accounts are created by the super-admin.

```typescript
// src/lib/api/auth.ts
import ky from 'ky'

export async function adminLogin(email: string, password: string): Promise<string> {
  const response = await ky.post('/api/v1/auth/admin/login', {
    json: { email, password },
  }).json<{ token: string }>()

  localStorage.setItem('vini_token', response.token)
  return response.token
}

export function getStoredToken(): string | null {
  return localStorage.getItem('vini_token')
}

export function clearToken(): void {
  localStorage.removeItem('vini_token')
}
```

The token is used for both REST calls and the MQTT password field. EMQX validates the same JWT. If the token expires while VINI is running, EMQX disconnects the MQTT client (connection rejected on next PING). VINI catches the disconnect, refreshes the token via REST, and reconnects.

---

## 9. Build and Deployment

```yaml
# vini/Dockerfile
FROM node:22-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
```

VINI is a static SPA served by nginx. Cloudflare sits in front for CDN and DDoS protection. VIDI REST API calls are proxied through nginx to avoid CORS:

```nginx
# nginx.conf
server {
    listen 80;

    location / {
        root /usr/share/nginx/html;
        try_files $uri $uri/ /index.html;
    }

    location /api/ {
        proxy_pass http://vidi-internal:8080;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

MQTT WebSocket connects directly to `wss://mqtt.adird.id:8084` — not proxied through nginx. Cloudflare proxies this subdomain with WebSocket support enabled.
