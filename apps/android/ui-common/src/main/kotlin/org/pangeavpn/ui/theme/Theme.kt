package org.pangeavpn.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.unit.dp

private val PangeaShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(12.dp),
    medium = RoundedCornerShape(16.dp),
    large = RoundedCornerShape(24.dp),
    extraLarge = RoundedCornerShape(28.dp),
)

private val PangeaDarkColors = darkColorScheme(
    primary = BrandOrange,
    onPrimary = BrandOrangeText,
    secondary = BrandOrangeHover,
    onSecondary = BrandOrangeText,
    background = DarkBackground,
    onBackground = DarkOnSurface,
    surface = DarkSurface,
    onSurface = DarkOnSurface,
    surfaceVariant = DarkSurfaceElevated,
    onSurfaceVariant = DarkOnSurfaceMuted,
    outline = DarkOutline,
    error = StateError,
    onError = BrandOrangeText,
)

private val PangeaLightColors = lightColorScheme(
    primary = BrandOrange,
    onPrimary = BrandOrangeText,
    secondary = BrandOrangeHover,
    onSecondary = BrandOrangeText,
    background = LightBackground,
    onBackground = LightOnSurface,
    surface = LightSurface,
    onSurface = LightOnSurface,
    surfaceVariant = LightSurfaceElevated,
    onSurfaceVariant = LightOnSurfaceMuted,
    outline = LightOutline,
    error = StateError,
    onError = BrandOrangeText,
)

@Composable
fun PangeaTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = if (darkTheme) PangeaDarkColors else PangeaLightColors,
        typography = Typography(),
        shapes = PangeaShapes,
        content = content,
    )
}
