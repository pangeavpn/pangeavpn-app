package org.pangeavpn.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.pangeavpn.core.Accent
import org.pangeavpn.core.ThemeChoice
import org.pangeavpn.core.ThemeStore
import org.pangeavpn.core.isDark
import org.pangeavpn.ui.R

private val PangeaShapes = Shapes(
    extraSmall = RoundedCornerShape(8.dp),
    small = RoundedCornerShape(12.dp),
    medium = RoundedCornerShape(16.dp),
    large = RoundedCornerShape(24.dp),
    extraLarge = RoundedCornerShape(28.dp),
)

// Every slot is filled, so a stock Material sheet, menu or dialog never shows the baseline purple.
private fun pangeaColors(dark: Boolean, accent: Accent): ColorScheme {
    val background = if (dark) DarkBackground else LightBackground
    val surface = if (dark) DarkSurface else LightSurface
    val elevated = if (dark) DarkSurfaceElevated else LightSurfaceElevated
    val text = if (dark) DarkOnSurface else LightOnSurface
    val wash = accent.color.copy(alpha = 0.15f)
    val base = if (dark) darkColorScheme() else lightColorScheme()
    return base.copy(
        primary = accent.color,
        onPrimary = OnAccent,
        primaryContainer = wash,
        onPrimaryContainer = text,
        inversePrimary = accent.color,
        secondary = accent.hover,
        onSecondary = OnAccent,
        secondaryContainer = wash,
        onSecondaryContainer = text,
        tertiary = accent.color,
        onTertiary = OnAccent,
        tertiaryContainer = wash,
        onTertiaryContainer = text,
        background = background,
        onBackground = text,
        surface = surface,
        onSurface = text,
        surfaceVariant = elevated,
        onSurfaceVariant = if (dark) DarkOnSurfaceMuted else LightOnSurfaceMuted,
        surfaceTint = Color.Transparent,
        inverseSurface = text,
        inverseOnSurface = background,
        error = StateError,
        onError = OnAccent,
        errorContainer = StateError.copy(alpha = 0.12f),
        onErrorContainer = StateError,
        outline = if (dark) DarkOutline else LightOutline,
        outlineVariant = if (dark) DarkOutlineSubtle else LightOutlineSubtle,
        surfaceBright = elevated,
        surfaceDim = background,
        surfaceContainer = surface,
        surfaceContainerHigh = elevated,
        surfaceContainerHighest = elevated,
        surfaceContainerLow = background,
        surfaceContainerLowest = background,
    )
}

/** Theme and accent as the composition sees them, with the setters the header and the
 *  Appearance page drive. Changes are stored and repaint every screen at once. */
@Immutable
class AppearanceController internal constructor(
    val theme: ThemeChoice,
    val accent: Accent,
    val dark: Boolean,
    private val store: ThemeStore?,
) {
    val accents: List<Accent> get() = Accent.entries

    fun set(choice: ThemeChoice) {
        store?.set(choice)
    }

    /** The header's tap: an explicit light or dark, never back to following the system. */
    fun toggle() = set(if (dark) ThemeChoice.LIGHT else ThemeChoice.DARK)

    fun setAccent(accent: Accent) {
        store?.setAccent(accent)
    }
}

val LocalAppearance = staticCompositionLocalOf {
    AppearanceController(ThemeChoice.SYSTEM, Accent.TERRA, dark = true, store = null)
}

/** --text-secondary has no Material slot, so it travels alongside the scheme. */
val LocalTextSecondary = staticCompositionLocalOf { DarkOnSurfaceSecondary }

/** LocalAppearance.current.toggle(), kept for the screens written before the controller. */
val LocalThemeToggle = staticCompositionLocalOf<() -> Unit> { {} }

/** Applies the Pangea palette with the stored theme and accent. Android TV reports no dark
 *  preference, so the TV app passes systemInDark = true to pin dark while the choice is System. */
@Composable
fun PangeaTheme(
    systemInDark: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    val context = LocalContext.current
    val store = remember(context) { ThemeStore(context) }
    val appearance by store.state.collectAsState()
    val dark = appearance.theme.isDark(systemInDark)
    val controller = remember(appearance, dark, store) {
        AppearanceController(appearance.theme, appearance.accent, dark, store)
    }
    val toggle = remember(controller) { { controller.toggle() } }
    val colors = remember(appearance.accent, dark) { pangeaColors(dark, appearance.accent) }

    CompositionLocalProvider(
        LocalAppearance provides controller,
        LocalTextSecondary provides if (dark) DarkOnSurfaceSecondary else LightOnSurfaceSecondary,
        LocalThemeToggle provides toggle,
    ) {
        MaterialTheme(
            colorScheme = colors,
            typography = Typography(),
            shapes = PangeaShapes,
            content = content,
        )
    }
}

@Composable
fun accentTitle(accent: Accent): String = stringResource(
    when (accent) {
        Accent.TERRA -> R.string.accent_terra
        Accent.PURPLE -> R.string.accent_purple
        Accent.OCEAN -> R.string.accent_ocean
        Accent.EMERALD -> R.string.accent_emerald
        Accent.ROSE -> R.string.accent_rose
    },
)

@Composable
fun themeChoiceTitle(choice: ThemeChoice): String = stringResource(
    when (choice) {
        ThemeChoice.SYSTEM -> R.string.appearance_theme_system
        ThemeChoice.LIGHT -> R.string.appearance_theme_light
        ThemeChoice.DARK -> R.string.appearance_theme_dark
    },
)
