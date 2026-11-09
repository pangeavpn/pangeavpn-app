package org.pangeavpn.tv.ui

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.unit.dp
import androidx.tv.material3.darkColorScheme
import androidx.tv.material3.lightColorScheme
import org.pangeavpn.ui.components.PangeaCardShape
import org.pangeavpn.ui.theme.LocalAppearance
import androidx.tv.material3.MaterialTheme as TvMaterialTheme

internal const val PRIVACY_URL = "https://pangeavpn.org/legal/privacy"
internal const val BILLING_URL = "https://pangeavpn.org/app/billing"

/** Televisions crop up to 5% of each edge, so every screen keeps its content inside these margins. */
fun Modifier.overscan(): Modifier = safeDrawingPadding().padding(horizontal = 48.dp, vertical = 27.dp)

/** tv-material's theme painted from the Pangea scheme, so its components never show library colours. */
@Composable
fun TvTheme(content: @Composable () -> Unit) {
    val scheme = MaterialTheme.colorScheme
    val dark = LocalAppearance.current.dark
    val colors = remember(scheme, dark) {
        (if (dark) darkColorScheme() else lightColorScheme()).copy(
            primary = scheme.primary,
            onPrimary = scheme.onPrimary,
            primaryContainer = scheme.primaryContainer,
            onPrimaryContainer = scheme.onPrimaryContainer,
            inversePrimary = scheme.inversePrimary,
            secondary = scheme.secondary,
            onSecondary = scheme.onSecondary,
            secondaryContainer = scheme.secondaryContainer,
            onSecondaryContainer = scheme.onSecondaryContainer,
            tertiary = scheme.tertiary,
            onTertiary = scheme.onTertiary,
            tertiaryContainer = scheme.tertiaryContainer,
            onTertiaryContainer = scheme.onTertiaryContainer,
            background = scheme.background,
            onBackground = scheme.onBackground,
            surface = scheme.surface,
            onSurface = scheme.onSurface,
            surfaceVariant = scheme.surfaceVariant,
            onSurfaceVariant = scheme.onSurfaceVariant,
            surfaceTint = scheme.surfaceTint,
            inverseSurface = scheme.inverseSurface,
            inverseOnSurface = scheme.inverseOnSurface,
            error = scheme.error,
            onError = scheme.onError,
            errorContainer = scheme.errorContainer,
            onErrorContainer = scheme.onErrorContainer,
            border = scheme.outline,
            borderVariant = scheme.outlineVariant,
            scrim = scheme.scrim,
        )
    }
    TvMaterialTheme(colorScheme = colors, content = content)
}

/** The region row's card, for TV rows that take focus without being one of the shared controls. */
@Composable
fun Modifier.tvCard(focused: Boolean, selected: Boolean = false): Modifier =
    clip(PangeaCardShape)
        .background(
            if (selected) MaterialTheme.colorScheme.primary.copy(alpha = 0.15f) else MaterialTheme.colorScheme.surface,
        )
        .border(
            width = if (focused) 2.dp else 1.dp,
            color = if (focused || selected) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outlineVariant,
            shape = PangeaCardShape,
        )

/** A requester whose target is not composed yet throws; on a TV that only means there is nothing to focus. */
fun FocusRequester.tryFocus() {
    runCatching { requestFocus() }
}

/** The link as an intent, or null: many televisions have no browser at all. */
fun browserIntent(context: Context, url: String): Intent? =
    Intent(Intent.ACTION_VIEW, Uri.parse(url)).takeIf { it.resolveActivity(context.packageManager) != null }

fun openIntent(context: Context, intent: Intent) {
    try {
        context.startActivity(intent)
    } catch (e: ActivityNotFoundException) {
        Unit
    }
}
