package id.adird.vici.ui.theme

import androidx.compose.ui.graphics.Color

// ─── Stormy Morning Palette ───────────────────────────────────────────────────
// Source: https://coolors.co — "Stormy morning"

val StormyPrimary     = Color(0xFF6A89A7)   // Slate blue      — buttons, interactive, active nav
val StormyPale        = Color(0xFFBDDDFC)   // Light periwinkle — containers, chips, dividers
val StormySky         = Color(0xFF88BDF2)   // Sky blue          — accent, secondary actions, FAB ring
val StormyNavy        = Color(0xFF384959)   // Dark navy         — text, toolbar bg, on-primary

// ─── Derived neutrals ────────────────────────────────────────────────────────

val BackgroundLight   = Color(0xFFF2F6FA)   // Very light blue-gray land base
val SurfaceWhite      = Color(0xFFFFFFFF)
val SurfaceContainer  = Color(0xFFEBF2F9)   // Card / bottom-sheet background
val SurfaceContainerHigh = Color(0xFFDCEAF5)

val OnPrimaryWhite    = Color(0xFFFFFFFF)
val OnNavy            = Color(0xFFFFFFFF)
val OnSurfaceText     = Color(0xFF384959)   // = StormyNavy
val OnSurfaceMuted    = Color(0xFF6A89A7)   // = StormyPrimary (secondary text)
val OnSurfaceDisabled = Color(0xFFBDDDFC)   // = StormyPale (disabled labels)
val OutlineColor      = Color(0xFFB0C8DA)

// ─── Semantic / status ────────────────────────────────────────────────────────

val SuccessGreen      = Color(0xFF3FAD6E)
val WarningAmber      = Color(0xFFF59E0B)
val ErrorRed          = Color(0xFFE05252)
val OnlineGreen       = Color(0xFF22C55E)   // driver dot — available
val OnTripAmber       = Color(0xFFF59E0B)   // driver dot — on trip

// ─── Map layer colors (used in map_style.json) ────────────────────────────────
// Documented here as Kotlin constants for reference; values live in map_style.json
// MAP_WATER         = StormySky         #88BDF2
// MAP_LAND          = BackgroundLight   #F2F6FA
// MAP_PARK          = Color(0xFFDAEDC8) #DAEDC8
// MAP_BUILDING      = Color(0xFFE4EEF4) #E4EEF4
// MAP_ROAD_MOTORWAY = StormySky         #88BDF2
// MAP_ROAD_PRIMARY  = SurfaceWhite      #FFFFFF (casing #B0C8DA)
// MAP_ROAD_MINOR    = SurfaceContainer  #EBF2F9 (casing #C8D8E8)
// MAP_LABEL         = StormyNavy        #384959
// MAP_WATER_LABEL   = StormyPrimary     #6A89A7
