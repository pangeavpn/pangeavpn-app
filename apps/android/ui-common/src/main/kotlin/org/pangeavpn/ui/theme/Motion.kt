package org.pangeavpn.ui.theme

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.TweenSpec
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.MotionDurationScale

/** One motion language on the map's own curve, so the map, hero, regions and button move as one. */
object Motion {
    const val FADE_OUT_MS = 120
    const val FADE_IN_MS = 220

    /** Small things appearing: notes, errors, toasts. */
    fun <T> quick(): TweenSpec<T> = tween(durationMillis = 200, easing = FastOutSlowInEasing)

    /** Colour and text changing in place. */
    fun <T> state(reduced: Boolean): TweenSpec<T> =
        tween(durationMillis = if (reduced) 200 else 350, easing = FastOutSlowInEasing)

    /** Whole screens swapping, such as splash to sign-in. */
    fun <T> screen(): TweenSpec<T> = tween(durationMillis = 300, easing = FastOutSlowInEasing)

    /** Layout that moves or resizes: sections sliding, rows arriving, lists opening. */
    fun <T> layout(reduced: Boolean): FiniteAnimationSpec<T> =
        if (reduced) tween(durationMillis = 200) else spring(dampingRatio = 0.86f, stiffness = 158f)
}

/** True while the system's "Remove animations" (animator scale 0) is on; follows a live change. */
@Composable
fun reducedMotion(): Boolean =
    rememberCoroutineScope().coroutineContext[MotionDurationScale]?.scaleFactor == 0f
