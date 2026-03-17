# ADIRD — Developer Makefile
# Apple Silicon (M1) + OrbStack

.PHONY: help dev-up dev-down dev-logs dev-health \
        run migrate-up migrate-down migrate-create \
        osrm-build osrm-up osrm-test \
        test lint tidy

# ─── Colors ───────────────────────────────────────────────────────
CYAN  := \033[36m
RESET := \033[0m

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "$(CYAN)%-20s$(RESET) %s\n", $$1, $$2}'

# ─── Docker Dev Stack ──────────────────────────────────────────────
dev-up: ## Start local dev stack (EMQX + PostgreSQL + Redis)
	docker compose -f docker-compose.dev.yml up -d emqx postgres redis
	@echo "\n✅ Services started:"
	@echo "  EMQX Dashboard → http://localhost:18083  (admin / adird_dev_123)"
	@echo "  PostgreSQL     → localhost:5432"
	@echo "  Redis          → localhost:6379"
	@echo "\nRun 'make run' to start VIDI backend"

dev-down: ## Stop all dev services
	docker compose -f docker-compose.dev.yml down

dev-logs: ## Tail logs from all dev services
	docker compose -f docker-compose.dev.yml logs -f

dev-health: ## Check health of all services
	@echo "Checking services..."
	@docker exec adird_emqx /opt/emqx/bin/emqx ctl status 2>/dev/null && echo "✅ EMQX: healthy" || echo "❌ EMQX: not running"
	@docker exec adird_postgres pg_isready -U adird 2>/dev/null && echo "✅ PostgreSQL: healthy" || echo "❌ PostgreSQL: not running"
	@docker exec adird_redis redis-cli ping 2>/dev/null | grep -q PONG && echo "✅ Redis: healthy" || echo "❌ Redis: not running"
	@curl -s http://localhost:8080/health 2>/dev/null | grep -q ok && echo "✅ VIDI API: healthy" || echo "⚠️  VIDI API: not running (start with 'make run')"

dev-reset: ## Wipe all dev data (volumes) and restart fresh
	docker compose -f docker-compose.dev.yml down -v
	docker compose -f docker-compose.dev.yml up -d emqx postgres redis

# ─── VIDI Backend ──────────────────────────────────────────────────
run: ## Run VIDI with hot reload (requires air)
	@which air > /dev/null 2>&1 || go install github.com/air-verse/air@latest
	air -c .air.toml

run-once: ## Run VIDI once (no hot reload)
	ENV_FILE=.env.dev go run ./cmd/server

# ─── Database Migrations ───────────────────────────────────────────
migrate-up: ## Apply all pending migrations
	migrate -path migrations -database "$$(grep DATABASE_URL .env.dev | cut -d= -f2-)" up

migrate-down: ## Rollback last migration
	migrate -path migrations -database "$$(grep DATABASE_URL .env.dev | cut -d= -f2-)" down 1

migrate-status: ## Show current migration version
	migrate -path migrations -database "$$(grep DATABASE_URL .env.dev | cut -d= -f2-)" version

migrate-create: ## Create new migration: make migrate-create name=add_drivers_index
	migrate create -ext sql -dir migrations -seq $(name)

# ─── OSRM ──────────────────────────────────────────────────────────
osrm-build: ## Download Jakarta OSM and build routing index (~10min first run)
	@chmod +x scripts/osrm-setup.sh
	./scripts/osrm-setup.sh

osrm-up: ## Start OSRM routing server (requires osrm-build first)
	docker compose -f docker-compose.dev.yml --profile osrm up -d osrm
	@echo "✅ OSRM → http://localhost:5000"

osrm-test: ## Test OSRM routing (Sudirman to Blok M)
	@echo "Testing route: Sudirman → Blok M"
	@curl -s "http://localhost:5000/route/v1/driving/106.8272,-6.1754;106.7991,-6.2438?overview=false" | \
	  python3 -c "import sys,json; r=json.load(sys.stdin); print(f'Distance: {r[\"routes\"][0][\"distance\"]/1000:.1f}km | Duration: {r[\"routes\"][0][\"duration\"]/60:.0f}min')"

# ─── Go Tools ──────────────────────────────────────────────────────
tidy: ## Run go mod tidy
	go mod tidy

test: ## Run all tests
	go test ./... -race -v

lint: ## Run golangci-lint
	@which golangci-lint > /dev/null 2>&1 || brew install golangci-lint
	golangci-lint run ./...

build: ## Build VIDI binary
	CGO_ENABLED=0 go build -o bin/vidi ./cmd/server

# ─── MQTT Testing ──────────────────────────────────────────────────
mqtt-test-gps: ## Publish a test GPS update (simulates driver)
	@which mosquitto_pub > /dev/null 2>&1 || brew install mosquitto
	mosquitto_pub -h localhost -p 1883 \
	  -t "adird/tracking/driver/test-driver-001" \
	  -m '{"lat":-6.1754,"lng":106.8272,"speed":35.2,"heading":270,"ts":$(shell date +%s)}' \
	  -q 0
	@echo "✅ GPS update published to adird/tracking/driver/test-driver-001"

mqtt-sub-all: ## Subscribe to all ADIRD topics (debug)
	@which mosquitto_sub > /dev/null 2>&1 || brew install mosquitto
	mosquitto_sub -h localhost -p 1883 -t "adird/#" -v
