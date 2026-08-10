package org.pangeavpn.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import org.pangeavpn.core.ThemeStore
import org.pangeavpn.core.isDark
import org.pangeavpn.core.toggled

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
    outlineVariant = DarkOutlineSubtle,
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
    outlineVariant = LightOutlineSubtle,
    error = StateError,
    onError = BrandOrangeText,
)

/** --text-secondary has no Material slot, so it travels alongside the scheme. */
val LocalTextSecondary = staticCompositionLocalOf { DarkOnSurfaceSecondary }

/** Lets the header flip the theme without every screen in between passing it down. */
val LocalThemeToggle = staticCompositionLocalOf<() -> Unit> { {} }

/**
 * Applies the Pangea palette. The theme follows [systemInDark] until the
 * header's toggle is used, after which the choice sticks across launches.
 * Android TV reports no dark preference, so the TV app pins it on instead.
 */
@Composable
fun PangeaTheme(
    systemInDark: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    val context = LocalContext.current
    val store = remember(context) { ThemeStore(context) }
    var choice by remember { mutableStateOf(store.choice()) }
    val darkTheme = choice.isDark(systemInDark)

    CompositionLocalProvider(
        LocalTextSecondary provides if (darkTheme) DarkOnSurfaceSecondary else LightOnSurfaceSecondary,
        LocalThemeToggle provides {
            val next = choice.toggled(systemInDark)
            store.set(next)
            choice = next
        },
    ) {
        MaterialTheme(
            colorScheme = if (darkTheme) PangeaDarkColors else PangeaLightColors,
            typography = Typography(),
            shapes = PangeaShapes,
            content = content,
        )
    }
}
