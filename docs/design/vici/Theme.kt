package id.adird.vici.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.material3.Typography
import androidx.compose.runtime.Composable
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

// ─── Typography ───────────────────────────────────────────────────────────────
// Plus Jakarta Sans — thematically fitting (Jakarta-based app), clean and modern.
// Add to app/build.gradle: implementation("androidx.compose.ui:ui-text-google-fonts:...")
// Or download and place in res/font/

val JakartaSans = FontFamily(
    // Fallback to default sans-serif if font not yet added to project
    Font(weight = FontWeight.Normal),
    Font(weight = FontWeight.Medium),
    Font(weight = FontWeight.SemiBold),
    Font(weight = FontWeight.Bold),
)

val AdirdTypography = Typography(
    displayLarge  = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Bold,     fontSize = 57.sp, lineHeight = 64.sp),
    displayMedium = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Bold,     fontSize = 45.sp, lineHeight = 52.sp),
    headlineLarge = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.SemiBold, fontSize = 32.sp, lineHeight = 40.sp),
    headlineMedium= TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.SemiBold, fontSize = 28.sp, lineHeight = 36.sp),
    headlineSmall = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.SemiBold, fontSize = 24.sp, lineHeight = 32.sp),
    titleLarge    = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.SemiBold, fontSize = 22.sp, lineHeight = 28.sp),
    titleMedium   = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Medium,   fontSize = 16.sp, lineHeight = 24.sp),
    titleSmall    = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Medium,   fontSize = 14.sp, lineHeight = 20.sp),
    bodyLarge     = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Normal,   fontSize = 16.sp, lineHeight = 24.sp),
    bodyMedium    = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Normal,   fontSize = 14.sp, lineHeight = 20.sp),
    bodySmall     = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Normal,   fontSize = 12.sp, lineHeight = 16.sp),
    labelLarge    = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Medium,   fontSize = 14.sp, lineHeight = 20.sp),
    labelMedium   = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Medium,   fontSize = 12.sp, lineHeight = 16.sp),
    labelSmall    = TextStyle(fontFamily = JakartaSans, fontWeight = FontWeight.Medium,   fontSize = 11.sp, lineHeight = 16.sp),
)

// ─── Color scheme ─────────────────────────────────────────────────────────────

private val LightColors = lightColorScheme(
    // Primary — slate blue (#6A89A7) for buttons, nav bar active item, progress
    primary                = StormyPrimary,
    onPrimary              = OnPrimaryWhite,
    primaryContainer       = StormyPale,        // #BDDFC — chip backgrounds, selected states
    onPrimaryContainer     = StormyNavy,

    // Secondary — sky blue (#88BDF2) for FAB, accent actions
    secondary              = StormySky,
    onSecondary            = StormyNavy,
    secondaryContainer     = StormyPale,
    onSecondaryContainer   = StormyNavy,

    // Tertiary — same navy used for dark toolbar areas
    tertiary               = StormyNavy,
    onTertiary             = OnPrimaryWhite,
    tertiaryContainer      = SurfaceContainer,
    onTertiaryContainer    = StormyNavy,

    // Error
    error                  = ErrorRed,
    onError                = OnPrimaryWhite,
    errorContainer         = Color(0xFFFFDAD6),
    onErrorContainer       = Color(0xFF410002),

    // Background & Surface
    background             = BackgroundLight,   // #F2F6FA — app background
    onBackground           = OnSurfaceText,     // #384959
    surface                = SurfaceWhite,      // #FFFFFF — cards, sheets
    onSurface              = OnSurfaceText,
    surfaceVariant         = SurfaceContainer,  // #EBF2F9 — input fills, dividers
    onSurfaceVariant       = OnSurfaceMuted,    // #6A89A7

    // Outlines
    outline                = OutlineColor,      // #B0C8DA
    outlineVariant         = StormyPale,        // #BDDFC

    // Inverse (used by snackbars)
    inverseSurface         = StormyNavy,
    inverseOnSurface       = OnPrimaryWhite,
    inversePrimary         = StormyPale,

    // Scrim (dialogs backdrop)
    scrim                  = Color(0xFF384959),
)

// ─── Theme entry point ────────────────────────────────────────────────────────

@Composable
fun AdirdTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = LightColors,
        typography  = AdirdTypography,
        content     = content,
    )
}
