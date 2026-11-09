package org.pangeavpn.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutLinearInEasing
import androidx.compose.animation.core.LinearOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.graphicsLayer
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.reducedMotion

/** Changes content by fading the old out before the new fades in, so two values never overprint.
 *  With animations removed in system settings the new value simply replaces the old. */
@Composable
fun <T> FadeSwap(
    value: T,
    modifier: Modifier = Modifier,
    content: @Composable (T) -> Unit,
) {
    val reduced = reducedMotion()
    var shown by remember { mutableStateOf(value) }
    val fade = remember { Animatable(1f) }

    // A newer value cancels this mid-fade, and the next run picks up from wherever the fade was.
    LaunchedEffect(value) {
        if (reduced) {
            shown = value
            fade.snapTo(1f)
            return@LaunchedEffect
        }
        if (value != shown) {
            fade.animateTo(0f, tween(Motion.FADE_OUT_MS, easing = FastOutLinearInEasing))
            shown = value
        }
        fade.animateTo(1f, tween(Motion.FADE_IN_MS, easing = LinearOutSlowInEasing))
    }

    Box(modifier = modifier.graphicsLayer { alpha = fade.value }) {
        content(shown)
    }
}
