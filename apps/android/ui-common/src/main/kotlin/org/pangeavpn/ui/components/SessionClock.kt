package org.pangeavpn.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import kotlinx.coroutines.delay
import org.pangeavpn.core.model.ConnState

/** The em dash the desktop hero uses for a fact with nothing to say yet. */
const val EM_DASH = "—"

/**
 * Elapsed time on the current connection, counted here because the daemon does
 * not report uptime. Reads as mm:ss, growing an hours field when it needs one.
 */
@Composable
fun sessionClock(state: ConnState): String {
    var elapsed by remember { mutableStateOf(0L) }

    LaunchedEffect(state) {
        if (state != ConnState.CONNECTED) {
            elapsed = 0
            return@LaunchedEffect
        }
        val startedAt = System.currentTimeMillis()
        while (true) {
            elapsed = (System.currentTimeMillis() - startedAt) / 1000
            delay(1_000)
        }
    }

    if (state != ConnState.CONNECTED) return EM_DASH
    return formatDuration(elapsed)
}

internal fun formatDuration(totalSeconds: Long): String {
    val safe = totalSeconds.coerceAtLeast(0)
    val hours = safe / 3600
    val minutes = (safe % 3600) / 60
    val seconds = safe % 60
    return if (hours > 0) {
        "%d:%02d:%02d".format(hours, minutes, seconds)
    } else {
        "%02d:%02d".format(minutes, seconds)
    }
}
