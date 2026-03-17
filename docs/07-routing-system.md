---
title: Routing System (OSRM)
tags: [routing, osrm, osm, navigation]
created: 2026-03-16
---

# Routing System — OSRM

> **See also**: [[03-map-navigation-stack]] | [[06-eta-prediction]] | [[09-infrastructure]]

---

## Why OSRM

| Routing Option | Cost | Motorcycle Support | Self-Hosted | Offline |
|----------------|------|--------------------|-------------|---------|
| Google Maps Directions | $$$$ | ❌ Car only | ❌ | ❌ |
| Mapbox Directions | $$ | ❌ | ❌ | ❌ |
| OSRM | **$0** | ✅ Custom profile | ✅ | ✅ |
| GraphHopper | $0 (OSS) | ✅ | ✅ | ✅ |
| Valhalla | $0 (OSS) | ✅ | ✅ | ✅ |

OSRM wins for MVP: fastest query time, simplest deployment, excellent Jakarta OSM coverage, and supports the motorcycle profile that is the core routing advantage.

---

## OSRM Setup

### Build Pipeline

```bash
# ─── On Hetzner CX22 (OSRM server) ───

# Step 1: Get Jakarta OSM data
wget https://download.geofabrik.de/asia/indonesia-latest.osm.pbf

# Step 2: Filter to Jakarta + 50km buffer (~150MB)
apt-get install -y osmium-tool
osmium extract \
  --bbox 106.5,-6.5,107.1,-6.0 \
  indonesia-latest.osm.pbf \
  -o jakarta.osm.pbf

# Step 3: Build OSRM routing index
# (use MLD algorithm: faster query time than CH for large graphs)
osrm-extract -p /profiles/motorcycle.lua jakarta.osm.pbf
osrm-partition jakarta.osrm
osrm-customize jakarta.osrm

# Step 4: Test routing query
curl "http://localhost:5000/route/v1/driving/106.8272,-6.1754;106.8200,-6.2000?overview=false"
```

### Docker Compose

```yaml
# /root/osrm/docker-compose.yml on Hetzner CX22
services:
  osrm:
    image: osrm/osrm-backend:latest
    volumes:
      - ./data:/data
      - ./profiles:/profiles
    command: osrm-routed --algorithm mld /data/jakarta.osrm --port 5000 --max-table-size 1000
    ports:
      - "5000:5000"    # bind to localhost, not public
    restart: unless-stopped
    mem_limit: 2g      # Jakarta region fits in ~1.2GB RAM

  # Weekly map update (cron)
  osrm-updater:
    image: osrm/osrm-backend:latest
    volumes:
      - ./data:/data
      - ./profiles:/profiles
      - ./scripts:/scripts
    entrypoint: /scripts/update_map.sh
    profiles: [update]  # only runs when explicitly triggered
```

### Weekly Map Update Script

```bash
#!/bin/bash
# /scripts/update_map.sh — run weekly via cron
set -e

echo "Downloading latest Jakarta OSM..."
wget -q https://download.geofabrik.de/asia/indonesia-latest.osm.pbf -O /tmp/indonesia.osm.pbf

osmium extract --bbox 106.5,-6.5,107.1,-6.0 \
  /tmp/indonesia.osm.pbf -o /data/jakarta-new.osm.pbf

echo "Building new OSRM index..."
osrm-extract -p /profiles/motorcycle.lua /data/jakarta-new.osm.pbf
osrm-partition /data/jakarta-new.osrm
osrm-customize /data/jakarta-new.osrm

# Atomic swap (zero downtime)
mv /data/jakarta.osrm /data/jakarta-old.osrm
mv /data/jakarta-new.osrm /data/jakarta.osrm

# Restart OSRM to pick up new index
docker compose restart osrm

echo "Map updated successfully: $(date)"
```

---

## Go OSRM Client

### Route Query

```go
type OSRMClient struct {
    baseURL string
    redis   *redis.Client
    http    *http.Client
}

type RouteResult struct {
    DistanceMeters  int
    DurationSeconds int
    Polyline        string
    Steps           []RouteStep
}

type RouteStep struct {
    Instruction string   // "Turn left onto Jl. Sudirman"
    DistanceM   int
    DurationS   int
    Bearing     int
    Maneuver    string   // "turn", "depart", "arrive"
}

func (c *OSRMClient) Route(ctx context.Context, origin, dest LatLng) (*RouteResult, error) {
    // Snap to 100m grid for cache hit rate optimization
    cacheKey := fmt.Sprintf("route:%s:%s",
        snapToGrid(origin.Lat, origin.Lng, 100),
        snapToGrid(dest.Lat, dest.Lng, 100))

    // Check cache first
    if cached, err := c.redis.Get(ctx, cacheKey).Result(); err == nil {
        var result RouteResult
        json.Unmarshal([]byte(cached), &result)
        return &result, nil
    }

    url := fmt.Sprintf(
        "%s/route/v1/driving/%f,%f;%f,%f?overview=full&geometries=geojson&steps=true",
        c.baseURL, origin.Lng, origin.Lat, dest.Lng, dest.Lat)

    resp, err := c.http.Get(url)
    if err != nil {
        return c.fallbackRoute(origin, dest), nil // graceful degradation
    }
    defer resp.Body.Close()

    var osrmResp struct {
        Routes []struct {
            Distance float64 `json:"distance"`
            Duration float64 `json:"duration"`
            Geometry struct {
                Coordinates [][]float64 `json:"coordinates"`
            } `json:"geometry"`
            Legs []struct {
                Steps []struct {
                    Distance    float64 `json:"distance"`
                    Duration    float64 `json:"duration"`
                    Maneuver    struct {
                        Type      string `json:"type"`
                        Bearing   int    `json:"bearing_after"`
                        Instruction string
                    } `json:"maneuver"`
                } `json:"steps"`
            } `json:"legs"`
        } `json:"routes"`
    }
    json.NewDecoder(resp.Body).Decode(&osrmResp)

    if len(osrmResp.Routes) == 0 {
        return c.fallbackRoute(origin, dest), nil
    }

    route := osrmResp.Routes[0]
    result := &RouteResult{
        DistanceMeters:  int(route.Distance),
        DurationSeconds: int(route.Duration),
        Polyline:        encodePolyline(route.Geometry.Coordinates),
        Steps:           parseSteps(route.Legs[0].Steps),
    }

    // Cache for 1 hour (road network doesn't change; traffic handled by ETA module)
    serialized, _ := json.Marshal(result)
    c.redis.Set(ctx, cacheKey, serialized, 1*time.Hour)

    return result, nil
}
```

### ETA-Only Query (Faster, No Geometry)

```go
func (c *OSRMClient) ETA(ctx context.Context, origin, dest LatLng) (time.Duration, error) {
    cacheKey := fmt.Sprintf("eta:%s:%s",
        snapToGrid(origin.Lat, origin.Lng, 100),
        snapToGrid(dest.Lat, dest.Lng, 100))

    if cached, _ := c.redis.Get(ctx, cacheKey).Result(); cached != "" {
        secs, _ := strconv.Atoi(cached)
        return time.Duration(secs) * time.Second, nil
    }

    // Use table service for faster ETA-only calculation
    url := fmt.Sprintf("%s/table/v1/driving/%f,%f;%f,%f?sources=0&destinations=1",
        c.baseURL, origin.Lng, origin.Lat, dest.Lng, dest.Lat)

    resp, err := c.http.Get(url)
    if err != nil {
        // Fallback: Haversine distance / average speed
        distKm := haversine(origin, dest) * 1.4 // road factor
        return time.Duration(distKm/30*60) * time.Minute, nil
    }
    defer resp.Body.Close()

    var tableResp struct {
        Durations [][]float64 `json:"durations"`
    }
    json.NewDecoder(resp.Body).Decode(&tableResp)

    if len(tableResp.Durations) == 0 {
        distKm := haversine(origin, dest) * 1.4
        return time.Duration(distKm/30*60) * time.Minute, nil
    }

    secs := int(tableResp.Durations[0][0])
    c.redis.Set(ctx, cacheKey, secs, 30*time.Minute) // ETAs cached shorter
    return time.Duration(secs) * time.Second, nil
}
```

### Fallback Route (OSRM Down)

```go
func (c *OSRMClient) fallbackRoute(origin, dest LatLng) *RouteResult {
    dist := haversine(origin, dest)
    roadDist := dist * 1.4 // road vs straight-line factor
    durationSec := int(roadDist / 8.33) // 30km/h average for Jakarta motorcycle

    return &RouteResult{
        DistanceMeters:  int(roadDist * 1000),
        DurationSeconds: durationSec,
        Polyline:        straightLinePolyline(origin, dest),
        Steps:           []RouteStep{}, // no turn-by-turn on fallback
    }
}
```

---

## Map Matching API

Used to snap GPS traces back to roads (reduce GPS drift in CBD):

```go
func (c *OSRMClient) MatchTrace(ctx context.Context, points []LatLng) ([]LatLng, error) {
    if len(points) < 2 {
        return points, nil
    }

    coords := buildCoordString(points) // "lng,lat;lng,lat;..."
    timestamps := buildTimestamps(points)

    url := fmt.Sprintf(
        "%s/match/v1/driving/%s?geometries=geojson&overview=full&timestamps=%s",
        c.baseURL, coords, timestamps)

    resp, err := c.http.Get(url)
    if err != nil || resp.StatusCode != 200 {
        return points, nil // return original on error (graceful)
    }
    defer resp.Body.Close()

    // Parse matched coordinates
    var matchResp struct {
        Matchings []struct {
            Geometry struct {
                Coordinates [][]float64 `json:"coordinates"`
            } `json:"geometry"`
        } `json:"matchings"`
    }
    json.NewDecoder(resp.Body).Decode(&matchResp)

    if len(matchResp.Matchings) == 0 {
        return points, nil
    }

    return coordinatesToLatLng(matchResp.Matchings[0].Geometry.Coordinates), nil
}
```

---

## Caching Strategy

```
Request: Route A → B
    │
    ▼
Redis cache key: route:{snap_A_100m}:{snap_B_100m}
    │
hit ├──► Return cached RouteResult (TTL: 1 hour)
    │
miss└──► OSRM HTTP query → store in Redis → return
```

### Cache Hit Rate Analysis

With 100m grid snapping:
- Trips starting/ending within 100m of same point share cache
- Expected cache hit rate: ~40–60% during peak hours (repeat popular routes)
- OSRM query rate: ~60–100/minute at 100 concurrent drivers → well within OSRM capacity

### Cache Invalidation

```
Route cache:  1 hour TTL (road network stable)
ETA cache:    30 minute TTL (traffic changes)
Fallback:     No cache (error state, always retry OSRM)
```

---

## OSRM API Endpoints Used

| Endpoint | Use Case | Frequency |
|----------|----------|-----------|
| `/route/v1/driving` | Full route with steps (navigation) | Per trip start |
| `/table/v1/driving` | ETA-only (no geometry) | Every dispatch scoring |
| `/match/v1/driving` | GPS trace snapping | Every 5 driver updates |
| `/nearest/v1/driving` | Snap passenger pin to road | Per order creation |

---

## Performance Benchmarks (Expected)

| Operation | Latency (Jakarta ~150MB index) |
|-----------|-------------------------------|
| `/route` (full geometry) | 10–50ms |
| `/table` (ETA only) | 5–20ms |
| `/match` (5 points) | 15–40ms |
| Memory footprint | ~1.2GB RAM |

Hetzner CX22 (2vCPU/4GB) comfortably handles 50+ requests/second for Jakarta region.

---

*See [[03-map-navigation-stack]] for tile serving, [[06-eta-prediction]] for ETA correction layer on top of OSRM.*
