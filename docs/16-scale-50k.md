---
title: Infrastructure Design — 50K Concurrent MQTT Connections
tags: [infrastructure, scale, emqx, mqtt, postgresql, redis, monitoring]
created: 2026-03-16
---

# Infrastructure Design — 50K Concurrent MQTT Connections

This document covers infrastructure sizing, cost analysis, and operational configuration for the ADIRD MVP scale target: 50,000 concurrent MQTT connections.

Related: [[14-mqtt-architecture]] | [[09-infrastructure]] | [[08-database-design]] | [[15-vini-dashboard]]

---

## 1. Scale Analysis

### 1.1 Connection Breakdown

| Client Type | Count | Notes |
|---|---|---|
| Drivers (VICI) | 30,000 | Always-on persistent sessions |
| Passengers (VICI) | 20,000 | Connected only during active booking or trip |
| VINI dashboards | ~10 | Ops team, wildcard subscribers |
| VIDI backend | 1–3 | `vidi_backend_server0N` service accounts |
| **Total** | **~50,013** | Rounds to 50K for sizing |

### 1.2 Message Throughput at MVP Scale

| Message Type | Rate Calculation | Messages/sec | QoS |
|---|---|---|---|
| Driver GPS updates | 30,000 drivers × 1 msg/4s | 7,500 | 0 |
| Trip location forwarding | 5,000 active trips × 1 msg/4s | 1,250 | 0 |
| Offer notifications | ~100/min peak | 2 | 1 |
| Driver status changes | ~50/s (login/logout events) | 50 | 1 |
| Zone surge updates | ~10/min | 0.2 | 0 |
| Trip status updates | ~100/min | 2 | 1 |
| **Total peak** | | **~8,804 msg/s** | |

**Rounded to 9,000 msg/s peak** for capacity planning.

### 1.3 Payload Size Estimates

| Message Type | Payload Bytes | Bandwidth Contribution |
|---|---|---|
| GPS tracking | ~80 bytes | 7,500 × 80 = 600 KB/s inbound |
| Trip location | ~60 bytes | 1,250 × 60 = 75 KB/s outbound |
| Offers | ~300 bytes | 2 × 300 = negligible |
| **Total** | | **~700 KB/s peak** = well under 1 Gbps network |

---

## 2. EMQX Capacity

EMQX Community Edition benchmarks (single node, published by EMQ Technologies):

| Metric | EMQX Benchmark | ADIRD Requirement | Headroom |
|---|---|---|---|
| Concurrent connections | 1,000,000+ | 50,000 | 20x |
| Message throughput | 1,000,000+ msg/s | 9,000 msg/s | 110x |
| Message latency p99 | <5ms | <100ms acceptable | — |
| Memory per connection | ~10KB | 50K × 10KB = 500MB | fits in 8GB |

**Conclusion:** A single EMQX node handles the MVP target with substantial headroom. No EMQX clustering required at this scale.

### 2.1 EMQX Server Sizing

For 50K connections at 9K msg/s peak:

| Server | vCPU | RAM | Cost/mo | Verdict |
|---|---|---|---|---|
| Hetzner CX22 | 2 | 4GB | $6 | Insufficient RAM for 50K connections |
| Hetzner CX32 | 4 | 8GB | $14 | Tight; workable for testing |
| Hetzner CX42 | 8 | 16GB | $28 | Recommended for MVP |
| Hetzner CX52 | 16 | 32GB | $56 | Overkill at MVP scale |

**Selected: Hetzner CX42 — 8vCPU, 16GB RAM, $28/mo.** Provides 2x memory headroom above the ~500MB baseline connection overhead.

---

## 3. Full Infrastructure Budget

| Component | Server | Monthly Cost | Notes |
|---|---|---|---|
| VIDI (Go API + Redis + PgBouncer) | Hetzner CX42 | $28 | All-in-one at MVP scale |
| EMQX Broker | Hetzner CX42 | $28 | Dedicated; isolated from API load |
| OSRM Routing | Hetzner CX22 | $6 | Jakarta road network only, ~1GB RAM |
| PostgreSQL | Hetzner CX32 | $14 | Dedicated DB node |
| Cloudflare | Free tier | $0 | CDN, DDoS, SSL termination, WebSocket proxy |
| OpenFreeMap tiles | Free | $0 | Self-hosted OSM tiles, no billing |
| FCM (push notifications) | Free | $0 | Google Firebase, no cost at MVP volume |
| Vonage SMS (OTP) | ~$0.05/SMS | ~$10 | Estimated 200 OTPs/day |
| Midtrans (payment gateway) | % per transaction | $0 fixed | Variable, not infrastructure |
| **Total fixed** | | **~$86/mo** | |
| **Total with SMS** | | **~$96/mo** | |

Target was under $200/mo. Actual: ~$96/mo with room for Redis Cloud or monitoring tools.

**Redis** runs on the same CX42 as VIDI at MVP scale. If Redis memory becomes a bottleneck (>4GB used), migrate to a dedicated CX32 for ~$14/mo additional.

---

## 4. EMQX Docker Setup

```yaml
# docker-compose.emqx.yml
# Deployed on Hetzner CX42 (emqx.adird.id)

services:
  emqx:
    image: emqx/emqx:5.7.0
    restart: unless-stopped
    mem_limit: 12g          # leave 4GB for OS and system processes
    environment:
      EMQX_NODE_NAME: "emqx@127.0.0.1"
      EMQX_CLUSTER__DISCOVERY_STRATEGY: manual

      # JWT authentication
      EMQX_AUTHENTICATION__1__MECHANISM: jwt
      EMQX_AUTHENTICATION__1__FROM: password
      EMQX_AUTHENTICATION__1__ALGORITHM: RS256
      EMQX_AUTHENTICATION__1__PUBLIC_KEY: "/opt/emqx/etc/jwt_public.pem"

      # Performance tuning for 50K connections
      EMQX_MQTT__MAX_CONNECTIONS: "100000"
      EMQX_MQTT__ALLOW_OVERRIDE: "true"

    ports:
      - "127.0.0.1:1883:1883"  # MQTT TCP — internal only (VIDI connects here)
      - "8883:8883"             # MQTT TLS — public (VICI mobile clients)
      - "8083:8083"             # MQTT over WebSocket — internal only
      - "8084:8084"             # MQTT over WebSocket TLS — public (VINI browser)
      - "127.0.0.1:18083:18083" # EMQX Dashboard — internal only, never expose publicly
    volumes:
      - emqx_data:/opt/emqx/data
      - emqx_log:/opt/emqx/log
      - ./config/emqx.conf:/opt/emqx/etc/emqx.conf:ro
      - ./config/acl.conf:/opt/emqx/etc/acl.conf:ro
      - ./certs/mqtt.adird.id.pem:/opt/emqx/etc/certs/cert.pem:ro
      - ./certs/mqtt.adird.id.key:/opt/emqx/etc/certs/key.pem:ro
      - ./certs/jwt_public.pem:/opt/emqx/etc/jwt_public.pem:ro
    healthcheck:
      test: ["CMD", "emqx", "ping"]
      interval: 30s
      timeout: 10s
      retries: 3

volumes:
  emqx_data:
  emqx_log:
```

Port exposure rationale:
- `1883` (plain MQTT): only accessible on localhost. VIDI connects via Docker internal network.
- `8883` (MQTT TLS): public. VICI Android connects here. TLS terminates at EMQX (not Cloudflare, since Cloudflare does not proxy MQTT TCP).
- `8084` (MQTT over WebSocket TLS): public. VINI browser connects here. Cloudflare can proxy this.
- `18083` (dashboard): never exposed publicly. Access via SSH tunnel only.

---

## 5. Connection Identity — Single Device per Account

See [[14-mqtt-architecture#8]] for full clientId format specification.

```
clientId = {role}_{userId}_{deviceId}

driver_abc123uuid_androidid789abc
passenger_xyz456uuid_androidid012def
vidi_backend_server01
vini_admin_browsersession1710000000
```

EMQX `allow_override = true` in `emqx.conf`:

```hocon
mqtt {
  allow_override = true
}
```

When a driver connects from a new device (or the app restarts), the new connection with the same clientId immediately kicks the old session. This prevents stale GPS streams and duplicate offer deliveries.

---

## 6. PgBouncer — PostgreSQL Connection Pooling

At 50K concurrent users, even a conservative 1% simultaneous REST API call rate produces 500 concurrent database connections. PostgreSQL default `max_connections = 100` is insufficient.

**PgBouncer in transaction mode** allows VIDI to use 500+ application-level connections while maintaining only 20 actual PostgreSQL server connections.

```yaml
# In docker-compose.vidi.yml on the VIDI server

services:
  pgbouncer:
    image: pgbouncer/pgbouncer:1.22
    restart: unless-stopped
    environment:
      DB_HOST: postgres.adird.id       # PostgreSQL server address
      DB_PORT: "5432"
      DB_USER: adird
      DB_PASSWORD: ${DB_PASSWORD}
      DB_NAME: adird_db
      POOL_MODE: transaction            # best for short-lived API queries
      MAX_CLIENT_CONN: "1000"           # VIDI can hold up to 1000 app-level connections
      DEFAULT_POOL_SIZE: "20"           # 20 actual PostgreSQL connections
      SERVER_RESET_QUERY: "DISCARD ALL"
      LOG_CONNECTIONS: "0"
      LOG_DISCONNECTIONS: "0"
    ports:
      - "127.0.0.1:5433:5432"           # VIDI connects to localhost:5433

  vidi:
    # ...
    environment:
      DATABASE_URL: "postgresql://adird:${DB_PASSWORD}@127.0.0.1:5433/adird_db"
```

**Transaction pooling** means a connection is only held during an active transaction. A VIDI goroutine handling an HTTP request borrows a connection, executes the query, returns it immediately. At typical API response times of 10–50ms, 20 server connections can serve hundreds of concurrent VIDI goroutines.

**Caveat:** transaction pooling does not support prepared statements or advisory locks across transactions. VIDI must use `pgx` in simple query mode or use `pgbouncer`-compatible patterns.

---

## 7. Redis Sizing

At 50K drivers, the Redis GEO set `drivers:online` holds 50K members. Each GEO member uses ~90 bytes in Redis.

| Data Structure | Keys/Members | Memory Estimate |
|---|---|---|
| `drivers:online` (GEO set) | 30,000 members | ~2.7MB |
| `driver:{id}` hashes | 30,000 hashes × ~100 bytes | ~3MB |
| `dispatch:lock:{id}` strings | ~200 concurrent (TTL 20s) | ~20KB |
| Offer channels | In-memory Go map, not Redis | — |
| Trip active mapping | ~5,000 entries | ~500KB |
| **Total** | | **~7MB** |

Redis memory use is negligible at this scale. The default 256MB limit on the shared instance is more than sufficient. No Redis Cluster required at MVP.

---

## 8. Monitoring at Scale

At 50K connections, anomalies become detectable as statistical deviations. Individual connection errors are expected; bulk deviations indicate infrastructure problems.

### 8.1 EMQX Built-In Dashboard (port 18083)

Key metrics visible in the EMQX dashboard:

| Metric | Normal Range | Alert Threshold |
|---|---|---|
| `emqx_connections_count` | ~50,000 | <40,000 (mass disconnect) |
| `emqx_messages_input_rate` | ~9,000/s | <1,000/s (GPS pipeline down) |
| `emqx_messages_dropped` (QoS 0) | Some acceptable | — |
| `emqx_messages_dropped` (QoS 1) | Should be 0 | >10/min |
| `emqx_broker_subscriptions` | ~100,000 | Growing unbounded |
| CPU usage | <40% at MVP | >80% sustained |
| Memory usage | <8GB | >13GB (approaching limit) |

### 8.2 Prometheus + Grafana

EMQX 5.x exposes a Prometheus endpoint at `:18083/api/v5/prometheus/stats`.

Key alerts to configure:

```yaml
# prometheus/alerts/emqx.yml

groups:
  - name: emqx
    rules:
      - alert: EMQXMassDisconnect
        expr: emqx_connections_count < 40000
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "EMQX connection count dropped below 40K (mass disconnect)"

      - alert: EMQXGPSPipelineDead
        expr: rate(emqx_messages_input_rate[5m]) < 1000
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "EMQX inbound message rate below 1K/s — GPS tracking likely offline"

      - alert: EMQXQoS1MessageDrop
        expr: increase(emqx_messages_dropped{qos="1"}[1m]) > 10
        labels:
          severity: warning
        annotations:
          summary: "QoS 1 messages being dropped — offer delivery at risk"

      - alert: EMQXSubscriptionLeak
        expr: deriv(emqx_broker_subscriptions[10m]) > 1000
        labels:
          severity: warning
        annotations:
          summary: "EMQX subscription count growing rapidly — possible topic leak"

      - alert: EMQXHighMemory
        expr: emqx_vm_memory_used_bytes > 13e9
        labels:
          severity: warning
        annotations:
          summary: "EMQX memory usage approaching container limit"
```

### 8.3 VIDI Metrics

VIDI exposes operational metrics at `/internal/metrics` (Prometheus format):

| Metric | Description |
|---|---|
| `vidi_dispatch_offers_sent_total` | Counter: offers published to MQTT |
| `vidi_dispatch_offers_accepted_total` | Counter: offers accepted by drivers |
| `vidi_dispatch_offer_timeout_total` | Counter: offers that expired without response |
| `vidi_dispatch_match_duration_seconds` | Histogram: time from order creation to driver assignment |
| `vidi_mqtt_publish_errors_total` | Counter: MQTT publish failures by topic prefix |
| `vidi_redis_geo_add_duration_seconds` | Histogram: Redis GEOADD latency |

### 8.4 Key Operational Runbooks

**Scenario: GPS pipeline down (message rate drops)**
1. Check EMQX dashboard — is `emqx_connections_count` normal?
2. If connections normal but message rate low: VICI app GPS disabled (background OS restriction)
3. If connections dropping: check EMQX container health, TLS cert expiry
4. If VIDI subscription dropped: check VIDI MQTT client reconnect logs

**Scenario: Offer delivery failures**
1. Check `vidi_dispatch_offer_timeout_total` spike
2. Check driver `emqx_connections_count` — are drivers connected?
3. Check QoS 1 queue depth in EMQX — messages pending for offline clients
4. Check Redis dispatch lock TTLs — are locks expiring correctly?

**Scenario: Mass driver disconnect**
1. Check EMQX server CPU and memory
2. Check network: `ping emqx.adird.id` from external
3. Check TLS certificate validity: `openssl s_client -connect mqtt.adird.id:8883`
4. Check `allow_override` — a bug causing mass reconnect storms would show as rapid connection churn

---

## 9. Scaling Beyond MVP

This section documents the scaling path if ADIRD grows beyond 50K concurrent connections. No action required at MVP.

| Scale | Connections | Action Required |
|---|---|---|
| MVP | 50K | Single EMQX node on CX42 |
| 200K | 200K | Add 3 EMQX nodes, enable EMQX clustering |
| 500K | 500K | EMQX cluster behind load balancer, dedicated Redis Cluster |
| 1M+ | 1M+ | EMQX Enterprise Edition, multi-region |

EMQX clustering uses a shared subscription model — VIDI subscribes to `adird/tracking/driver/+` on one node; EMQX cluster routes the message to the subscribed node. No changes to VIDI or VICI code at scaling step.

PostgreSQL scaling path:
- MVP: single PG node + PgBouncer
- 5x growth: read replica for analytics queries (VINI reads from replica)
- 20x growth: Citus or partition-by-zone sharding

Redis scaling path:
- MVP: single Redis instance, ~7MB for location data
- 10x growth: Redis Cluster if memory exceeds 1GB or latency degrades
- At no foreseeable scale does Redis become the bottleneck before EMQX or PostgreSQL do
