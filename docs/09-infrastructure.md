---
title: Infrastructure and Deployment
tags: [infrastructure, hetzner, docker, devops, ci-cd]
created: 2026-03-16
---

# Infrastructure and Deployment

> **See also**: [[02-system-architecture]] | [[08-database-design]] | [[13-roadmap]]

---

## Budget Overview

Total monthly cost: **~$86–97/month** for 50K concurrent connections (well under $200 budget)

| Resource | Server | Cost/mo | Purpose |
|----------|--------|---------|---------|
| VIDI (Go API) | Hetzner CX42 (8vCPU/16GB) | $28 | API + Redis + PgBouncer |
| EMQX Broker | Hetzner CX42 (8vCPU/16GB) | $28 | 50K MQTT connections |
| PostgreSQL | Hetzner CX32 (4vCPU/8GB) | $14 | Dedicated database |
| OSRM | Hetzner CX22 (2vCPU/4GB) | $6 | Jakarta routing |
| Cloudflare Free | — | $0 | CDN, DDoS, SSL |
| OpenFreeMap | Hosted OSM tiles | $0 | Map tiles (VICI) |
| Firebase FCM | — | $0 | Push notifications |
| Midtrans | ~0.7% per txn | $0 fixed | Payment |
| Vonage SMS | ~$0.05/SMS | ~$10 | OTP at scale |
| **Total** | | **~$86/mo** | ✅ Under $200 |

> See [[16-scale-50k]] for detailed capacity analysis and scaling path.

> **Hetzner Singapore** (sgp1) is preferred over Frankfurt for lower latency from Jakarta (~100ms vs ~200ms). Check availability.

---

## Server 1: CX32 — Main Application

```
CX32 (4vCPU / 8GB RAM / 80GB SSD)
├── Nginx (reverse proxy, SSL termination)
├── Go API Server (adird-api)
├── Redis 7 (driver tracking, sessions, cache)
└── PostgreSQL 16 (all persistent data)
```

### Docker Compose

```yaml
# /app/docker-compose.yml

services:
  api:
    image: adird-api:latest
    build:
      context: .
      dockerfile: Dockerfile
    ports:
      - "127.0.0.1:8080:8080"  # only accessible via nginx, not public
    environment:
      DATABASE_URL: postgres://adird:${DB_PASSWORD}@postgres:5432/adird_db?sslmode=disable
      REDIS_URL: redis://redis:6379
      OSRM_URL: http://${OSRM_SERVER_IP}:5000
      FCM_SERVICE_ACCOUNT_JSON: /run/secrets/fcm_sa
      MIDTRANS_SERVER_KEY: ${MIDTRANS_SERVER_KEY}
      MIDTRANS_CLIENT_KEY: ${MIDTRANS_CLIENT_KEY}
      JWT_PRIVATE_KEY_PATH: /run/secrets/jwt_private_key
      VONAGE_API_KEY: ${VONAGE_API_KEY}
      VONAGE_API_SECRET: ${VONAGE_API_SECRET}
      APP_ENV: production
    secrets:
      - fcm_sa
      - jwt_private_key
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_started
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  postgres:
    image: postgres:16-alpine
    volumes:
      - pgdata:/var/lib/postgresql/data
    environment:
      POSTGRES_DB: adird_db
      POSTGRES_USER: adird
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "adird", "-d", "adird_db"]
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    command: >
      redis-server
      --maxmemory 512mb
      --maxmemory-policy allkeys-lru
      --appendonly yes
      --appendfsync everysec
    volumes:
      - redisdata:/data
    restart: unless-stopped

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx/nginx.conf:/etc/nginx/nginx.conf:ro
      - /etc/letsencrypt:/etc/letsencrypt:ro
      - /var/www/certbot:/var/www/certbot:ro
    depends_on:
      - api
    restart: unless-stopped

volumes:
  pgdata:
  redisdata:

secrets:
  fcm_sa:
    file: ./secrets/fcm_service_account.json
  jwt_private_key:
    file: ./secrets/jwt_private_key.pem
```

### Nginx Configuration

```nginx
# /app/nginx/nginx.conf

events { worker_connections 1024; }

http {
    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api:10m rate=30r/m;
    limit_req_zone $binary_remote_addr zone=auth:10m rate=5r/m;

    upstream api_backend {
        server api:8080;
    }

    # Redirect HTTP to HTTPS
    server {
        listen 80;
        server_name api.adird.id;
        return 301 https://$host$request_uri;
    }

    server {
        listen 443 ssl;
        server_name api.adird.id;

        ssl_certificate     /etc/letsencrypt/live/api.adird.id/fullchain.pem;
        ssl_certificate_key /etc/letsencrypt/live/api.adird.id/privkey.pem;
        ssl_protocols       TLSv1.2 TLSv1.3;

        # WebSocket support
        location /ws/ {
            proxy_pass http://api_backend;
            proxy_http_version 1.1;
            proxy_set_header Upgrade $http_upgrade;
            proxy_set_header Connection "upgrade";
            proxy_set_header Host $host;
            proxy_read_timeout 3600;   # keep WS connections alive for 1 hour
            proxy_send_timeout 3600;
        }

        # Auth endpoints: stricter rate limit (prevent OTP abuse)
        location /auth/ {
            limit_req zone=auth burst=3 nodelay;
            proxy_pass http://api_backend;
        }

        # General API
        location / {
            limit_req zone=api burst=20 nodelay;
            proxy_pass http://api_backend;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        }
    }
}
```

---

## Server 2: CX22 — OSRM Routing

```
CX22 (2vCPU / 4GB RAM / 40GB SSD)
└── OSRM Backend (motorcycle profile, Jakarta)
```

```yaml
# /root/osrm/docker-compose.yml

services:
  osrm:
    image: osrm/osrm-backend:latest
    volumes:
      - ./data:/data
      - ./profiles:/profiles
    command: osrm-routed --algorithm mld /data/jakarta.osrm --port 5000
    ports:
      - "127.0.0.1:5000:5000"  # internal only, accessed by CX32 via private network
    restart: unless-stopped
    mem_limit: 2g
```

**Security note**: Bind OSRM to internal IP only. Use Hetzner Private Network to connect CX32 ↔ CX22 without exposing OSRM to the internet.

---

## Dockerfile

```dockerfile
# Multi-stage build: small production image
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o adird-api ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates curl
WORKDIR /app
COPY --from=builder /app/adird-api .
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s CMD curl -f http://localhost:8080/health || exit 1
CMD ["./adird-api"]
```

---

## CI/CD with GitHub Actions

```yaml
# .github/workflows/deploy.yml

name: Deploy to Hetzner
on:
  push:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: go test ./... -race -coverprofile=coverage.out
      - run: go vet ./...

  deploy:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Build Docker image
        run: |
          docker build -t adird-api:${{ github.sha }} .
          docker save adird-api:${{ github.sha }} | gzip > /tmp/image.tar.gz

      - name: Copy image to server
        uses: appleboy/scp-action@v0.1.7
        with:
          host: ${{ secrets.HETZNER_IP }}
          username: deploy
          key: ${{ secrets.SSH_PRIVATE_KEY }}
          source: /tmp/image.tar.gz
          target: /tmp/

      - name: Deploy on server
        uses: appleboy/ssh-action@v1.0.3
        with:
          host: ${{ secrets.HETZNER_IP }}
          username: deploy
          key: ${{ secrets.SSH_PRIVATE_KEY }}
          script: |
            # Load new image
            docker load < /tmp/image.tar.gz

            # Tag as latest
            docker tag adird-api:${{ github.sha }} adird-api:latest

            # Run database migrations
            docker run --rm --network app_default \
              --env DATABASE_URL=$DATABASE_URL \
              adird-api:latest \
              ./adird-api migrate

            # Rolling restart (zero-downtime)
            docker compose -f /app/docker-compose.yml up -d api

            # Verify health
            sleep 10
            curl -f https://api.adird.id/health || exit 1

            # Cleanup old images
            docker image prune -f
```

---

## Initial Server Setup

```bash
# Run once on fresh Hetzner CX32
#!/bin/bash

# System updates
apt-get update && apt-get upgrade -y

# Install Docker
curl -fsSL https://get.docker.com | sh
systemctl enable docker

# Create deploy user (CI/CD deploys as this user, not root)
useradd -m -s /bin/bash deploy
usermod -aG docker deploy

# Create app directory
mkdir -p /app/nginx /app/secrets
chown -R deploy:deploy /app

# Firewall: allow SSH, HTTP, HTTPS only
ufw allow ssh
ufw allow 80
ufw allow 443
ufw enable

# SSL certificate via Certbot
snap install certbot --classic
certbot certonly --standalone -d api.adird.id

# Copy .env file with secrets
# (done manually, not via CI/CD)
```

---

## Environment Variables

```bash
# /app/.env (never committed to git)

# Database
DB_PASSWORD=<strong_random_password>

# JWT
# Generate: openssl genrsa -out jwt_private_key.pem 2048

# OSRM (Hetzner private network IP)
OSRM_SERVER_IP=10.0.0.2

# Firebase FCM
# Place service account JSON in /app/secrets/fcm_service_account.json

# Midtrans
MIDTRANS_SERVER_KEY=Mid-server-xxxxx
MIDTRANS_CLIENT_KEY=Mid-client-xxxxx
MIDTRANS_ENV=production  # or sandbox

# Vonage SMS
VONAGE_API_KEY=xxxxx
VONAGE_API_SECRET=xxxxx
VONAGE_FROM=ADIRD

# App
APP_ENV=production
LOG_LEVEL=info
```

---

## Monitoring (Free Tier)

```
Grafana Cloud Free → metrics dashboard
  └── Prometheus scrape from Go /metrics endpoint
      ├── API request rate, latency p50/p95/p99
      ├── WebSocket connection count
      ├── Dispatch success rate
      └── OSRM query latency

Uptime monitoring: BetterStack free tier
  └── Alert on: /health endpoint down, OSRM down

Logs: Hetzner server journald
  └── docker compose logs -f (for debugging)
```

### Key Metrics to Watch

| Metric | Alert Threshold |
|--------|----------------|
| API p95 latency | > 500ms |
| WebSocket connections | < 50% of expected drivers |
| Dispatch success rate | < 75% |
| PostgreSQL connection pool | > 80% used |
| Redis memory | > 400MB (of 512MB limit) |
| OSRM p95 latency | > 200ms |

---

## Scaling Path

### At ~300 Concurrent Drivers

1. Move PostgreSQL to **Supabase** managed (~$25/mo) — eliminates manual backup burden, adds connection pooling
2. Dedicate CX22 to Redis — resolves memory contention with API server
3. Upgrade API server to CX42 (8vCPU/16GB) — ~$28/mo

**Estimated cost**: ~$60–70/mo

### At ~1000 Concurrent Drivers

1. Add second OSRM replica behind nginx round-robin (stateless, trivial to scale)
2. Split Go monolith: extract Dispatch Engine as a separate service (now the module boundary pays off)
3. Introduce Redis Sentinel for high availability
4. Consider Kafka for trip event streaming (analytics, notifications fan-out)

**Estimated cost**: ~$150–200/mo

---

*See [[02-system-architecture]] for service design, [[13-roadmap]] for when to tackle each scaling milestone.*
