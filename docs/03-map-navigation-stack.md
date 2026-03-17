---
title: Map and Navigation Stack
tags: [maps, osm, osrm, maplibre, navigation]
created: 2026-03-16
---

# Map and Navigation Stack

> **See also**: [[02-system-architecture]] | [[07-routing-system]] | [[04-dispatch-algorithm]]

---

## Stack Overview

```
Mobile (Android)
  └── MapLibre Android SDK
        └── Tiles ← OpenFreeMap (free, hosted OSM)

Backend
  └── OSRM Server (self-hosted, Hetzner CX22)
        └── Jakarta OSM data (from Geofabrik)
        └── Custom motorcycle.lua profile

Map Data Pipeline
  Geofabrik → osmium-tool → jakarta.osm.pbf → OSRM index
```

**Zero cost for maps.** No Google Maps API. No HERE Maps. No Mapbox paid tier.

---

## OSM Data Pipeline

### Download and Filter Jakarta Region

```bash
# 1. Download Indonesia extract from Geofabrik (~800MB)
wget https://download.geofabrik.de/asia/indonesia-latest.osm.pbf

# 2. Filter to Jakarta + 50km buffer using osmium-tool
osmium extract \
  --bbox 106.5,-6.5,107.1,-6.0 \
  indonesia-latest.osm.pbf \
  -o jakarta.osm.pbf

# 3. Verify extract size and content
osmium fileinfo jakarta.osm.pbf
# Expected: ~150-200MB for greater Jakarta area
```

### Why Geofabrik?
- Updated daily from OpenStreetMap
- Free to download
- Pre-clipped regional extracts available
- Indonesia coverage is excellent (OSM community is active in Jakarta)

---

## OSRM Custom Motorcycle Profile

The standard OSRM car profile **excludes footways and service roads** that motorcycles use constantly in Jakarta. Create `profiles/motorcycle.lua`:

```lua
-- motorcycle.lua - Jakarta Ojek Profile
-- Key difference: allows access to footways, paths, service roads
-- Excludes: toll roads (motorcycles banned)

speed_profile = {
  motorway        = 70,   -- expressway (if allowed)
  trunk           = 55,
  primary         = 45,   -- Jl. Sudirman, Jl. Thamrin
  secondary       = 35,   -- main roads
  tertiary        = 25,   -- local roads
  residential     = 20,   -- housing streets
  service         = 15,   -- service roads, parking access
  footway         = 10,   -- KEY: ojek uses pedestrian paths
  path            = 10,   -- gang/alley access
  living_street   = 15,   -- shared living streets
  unclassified    = 20,
  track           = 10,
}

-- Allow motorcycle access to footways and service roads
access_tag_whitelist = Set {
  'yes', 'permissive', 'designated', 'motorcycle', 'vehicle'
}

-- Blacklist toll highways (motorcycles banned in Jakarta)
access_tag_blacklist = Set {
  'no', 'private', 'motorway'  -- motorway = toll in Indonesian OSM tagging
}

-- Turn restrictions (honor Indonesian traffic rules)
use_turn_restrictions = true
```

### Build OSRM Index

```bash
# On Hetzner CX22 (OSRM server)
osrm-extract -p profiles/motorcycle.lua jakarta.osm.pbf
osrm-partition jakarta.osrm
osrm-customize jakarta.osrm

# Start routing server (MLD algorithm = faster queries than CH)
osrm-routed --algorithm mld jakarta.osrm --port 5000 --max-table-size 1000
```

### Docker Setup (Recommended)

```yaml
# docker-compose.osrm.yml
services:
  osrm:
    image: osrm/osrm-backend:latest
    volumes:
      - ./data:/data
    command: osrm-routed --algorithm mld /data/jakarta.osrm --port 5000
    ports:
      - "5000:5000"
    restart: unless-stopped
    mem_limit: 2g  # Jakarta region fits in ~1.2GB RAM
```

---

## Map Tile Serving

### OpenFreeMap (MVP — $0)
Use **[OpenFreeMap](https://openfreemap.org)** — fully hosted OSM tiles, no API key required.

```kotlin
// MapLibre style URL (no API key needed)
val TILE_STYLE = "https://tiles.openfreemap.org/styles/liberty"
```

**Do NOT self-host tiles for MVP.** A tile server requires:
- 50GB+ storage for Indonesia tiles
- Significant CPU for tile rendering
- Neither fits the MVP budget constraint

### Future: Self-Hosted PMTiles (Post-MVP)
When you want full control and offline support:
```bash
# Download Jakarta PMTiles (vector tiles, single file)
# Host on Hetzner CX32 using nginx
# MapLibre can serve from local device storage for offline use
```

---

## MapLibre Android Integration

### Initialization

```kotlin
class MapActivity : AppCompatActivity() {
    private lateinit var mapView: MapView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        mapView = MapView(this, MapInitOptions(
            context = this,
            styleUri = "https://tiles.openfreemap.org/styles/liberty",
            cameraOptions = CameraOptions.Builder()
                .center(Point.fromLngLat(106.8272, -6.1754)) // Jakarta center
                .zoom(12.0)
                .build()
        ))
        setContentView(mapView)

        mapView.getMapboxMap().loadStyle(styleUri) { style ->
            setupDriverLayer(style)
        }
    }
}
```

### Driver Navigation Camera (Bearing-Up Mode)

```kotlin
private fun updateDriverCamera(location: Location) {
    mapView.getMapboxMap().setCamera(
        CameraOptions.Builder()
            .center(Point.fromLngLat(location.longitude, location.latitude))
            .zoom(16.0)
            .bearing(location.bearing.toDouble()) // rotate map to driver's heading
            .pitch(45.0)  // slight tilt for navigation feel
            .build()
    )
}
```

### Draw Route Polyline

```kotlin
fun drawRoute(style: Style, polylinePoints: List<Point>) {
    val routeSource = GeoJsonSource.Builder("route-source")
        .feature(Feature.fromGeometry(LineString.fromLngLats(polylinePoints)))
        .build()

    val routeLayer = LineLayer("route-layer", "route-source").apply {
        lineColor(Color.parseColor("#4A90D9"))
        lineWidth(6.0)
        lineCap(LineCap.ROUND)
        lineJoin(LineJoin.ROUND)
    }

    style.addSource(routeSource)
    style.addLayer(routeLayer)
}
```

---

## Rerouting Logic

### Detection (Driver App)

Every 2 consecutive GPS samples, compute distance from current position to nearest point on the planned polyline:

```kotlin
fun isOffRoute(currentLocation: Location, routePoints: List<LatLng>): Boolean {
    val nearestPoint = routePoints.minByOrNull { point ->
        distanceBetween(currentLocation.latitude, currentLocation.longitude,
                        point.latitude, point.longitude)
    }
    val distanceToRoute = distanceBetween(
        currentLocation.latitude, currentLocation.longitude,
        nearestPoint!!.latitude, nearestPoint.longitude
    )
    return distanceToRoute > 50.0 // meters
}
```

### Trigger Reroute (API Call)

```kotlin
if (consecutiveOffRouteCount >= 2) {
    apiService.rerouteDriver(
        currentLat = location.latitude,
        currentLng = location.longitude,
        destinationLat = trip.dropoffLat,
        destinationLng = trip.dropoffLng
    ).also { newRoute ->
        drawRoute(mapStyle, newRoute.polylinePoints)
        updateNavigationInstructions(newRoute.steps)
        consecutiveOffRouteCount = 0
    }
}
```

---

## Map Matching (GPS Snap to Road)

In Sudirman CBD, GPS drift of 20–50m causes the driver dot to appear inside buildings. Use OSRM `/match` API to snap the GPS trace back to the road network:

```go
// Go backend: apply map matching before broadcasting to passenger
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
    // ... parse matched coordinates, return snapped positions
}
```

Apply map matching to the driver's **last 5 GPS points** before displaying position on the passenger map. Eliminates the "driver floating in a building" artifact.

---

## Offline Map Support (Future Feature)

```kotlin
// Pre-download Jakarta zones on WiFi
val offlineManager = OfflineManager(mapView.getMapboxMap())

fun downloadJakartaZone(zoneName: String, bounds: CoordinateBounds) {
    val options = TilesetDescriptorOptions.Builder()
        .styleURI(TILE_STYLE)
        .minZoom(10)
        .maxZoom(16)
        .build()

    val descriptor = offlineManager.createTilesetDescriptor(options)
    offlineManager.downloadTileRegion(
        id = "jakarta-$zoneName",
        options = TileRegionLoadOptions.Builder()
            .geometry(bounds.toPolygon())
            .descriptors(listOf(descriptor))
            .build(),
        progress = { /* show download progress */ },
        completion = { /* available offline */ }
    )
}
```

Navigation continues through Semanggi flyover or Senayan underpass when signal drops.

---

*See [[07-routing-system]] for OSRM deployment details, [[05-realtime-tracking]] for GPS tracking implementation.*
