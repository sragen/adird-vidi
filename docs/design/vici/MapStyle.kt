package id.adird.vici.ui.map

/**
 * VICI map configuration for MapLibre Android SDK.
 *
 * Usage in a Fragment/Activity:
 *
 *   val mapView = binding.mapView
 *   mapView.getMapAsync { map ->
 *       MapStyleConfig.apply(map) { style ->
 *           // Style loaded — add your layers/markers here
 *       }
 *   }
 *
 * Gradle dependency (add to app/build.gradle):
 *   implementation("org.maplibre.gl:android-sdk:11.+")
 *
 * In Manifest (no API key needed — OpenFreeMap is free):
 *   No special permissions beyond INTERNET required.
 *
 * Asset file: app/src/main/assets/map_style.json  ← copy docs/design/vici/map_style.json here
 */
object MapStyleConfig {

    /**
     * Load the embedded Stormy Morning style from assets.
     * This is the recommended approach — works offline after first cache.
     */
    const val ASSET_STYLE = "asset://map_style.json"

    /**
     * Fallback: load the OpenFreeMap liberty base style directly.
     * Use this if the asset file is not yet embedded.
     */
    const val REMOTE_LIBERTY = "https://tiles.openfreemap.org/styles/liberty"

    /** Jakarta center coordinates */
    val JAKARTA_LAT_LNG = Pair(-6.2088, 106.8456)
    const val DEFAULT_ZOOM = 14.0

    /**
     * Camera settings for the default driver view.
     * Tilt slightly for depth perception while navigating.
     */
    const val NAVIGATION_TILT   = 45.0   // degrees
    const val NAVIGATION_ZOOM   = 16.0
    const val OVERVIEW_ZOOM     = 12.0
    const val OVERVIEW_TILT     = 0.0
}

// ─── MapLibre Layer IDs present in map_style.json ────────────────────────────
// Use these constants when manipulating layers at runtime.

object MapLayerIds {
    const val BACKGROUND          = "background"
    const val WATER               = "water"
    const val WATERWAY            = "waterway"
    const val PARK                = "park"
    const val BUILDING            = "building"
    const val ROAD_MOTORWAY       = "road-motorway"
    const val ROAD_MOTORWAY_CASE  = "road-motorway-casing"
    const val ROAD_PRIMARY        = "road-primary"
    const val ROAD_SECONDARY      = "road-secondary"
    const val ROAD_TERTIARY       = "road-tertiary"
    const val ROAD_MINOR          = "road-minor"
    const val PLACE_CITY          = "place-city"
    const val PLACE_TOWN          = "place-village-town"
}

// ─── GeoJSON source IDs for app-level overlays ───────────────────────────────
// These are ADDED by the app at runtime (not in map_style.json).

object MapSourceIds {
    const val DRIVER_LOCATION     = "driver-location"    // current driver's own dot
    const val ROUTE_POLYLINE      = "route-polyline"     // active trip route
    const val PICKUP_MARKER       = "pickup-marker"      // pickup pin
    const val DROPOFF_MARKER      = "dropoff-marker"     // dropoff pin
    const val PASSENGER_LOCATION  = "passenger-location" // passenger dot (en_route phase)
}

object MapAppLayerIds {
    const val ROUTE_LINE          = "app-route-line"
    const val ROUTE_LINE_CASING   = "app-route-line-casing"
    const val DRIVER_DOT          = "app-driver-dot"
    const val PICKUP_ICON         = "app-pickup-icon"
    const val DROPOFF_ICON        = "app-dropoff-icon"
    const val PASSENGER_DOT       = "app-passenger-dot"
}

// ─── App-level map colors (applied programmatically) ─────────────────────────

object MapAppColors {
    const val ROUTE_LINE          = "#6A89A7"   // primary blue-gray
    const val ROUTE_LINE_CASING   = "#BDDFC"    // pale blue outline
    const val DRIVER_DOT          = "#22C55E"   // green — driver's own position
    const val PICKUP_PIN          = "#384959"   // navy
    const val DROPOFF_PIN         = "#88BDF2"   // sky blue
    const val PASSENGER_DOT       = "#F59E0B"   // amber — passenger location
}
