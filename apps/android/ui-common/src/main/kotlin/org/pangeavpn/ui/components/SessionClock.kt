package org.pangeavpn.ui.components

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import java.util.Locale
import kotlinx.coroutines.delay
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.TunnelStatus

/** The em dash the desktop hero uses for a fact with nothing to say yet. */
const val EM_DASH = "—"

/** Time on the VPN session, ticking each second; the service keeps the start across switches and reconnects. */
@Composable
fun sessionClock(status: TunnelStatus): String {
    val since = status.connectedSince
    val running = status.state == ConnState.CONNECTED && since > 0
    val now by produceState(System.currentTimeMillis(), running, since) {
        while (running) {
            value = System.currentTimeMillis()
            // Lands each tick on a whole second of the session, so the display never skips one.
            delay(1_000 - (value - since).mod(1_000L))
        }
    }
    return sessionClockText(status.state, since, now)
}

fun sessionClockText(state: ConnState, connectedSince: Long, nowMillis: Long): String {
    if (state != ConnState.CONNECTED || connectedSince <= 0) return EM_DASH
    return formatDuration((nowMillis - connectedSince) / 1_000)
}

/** mm:ss, growing an hours field from one hour; always ASCII digits, as on iOS. */
fun formatDuration(totalSeconds: Long): String {
    val safe = totalSeconds.coerceAtLeast(0)
    val hours = safe / 3600
    val minutes = (safe % 3600) / 60
    val seconds = safe % 60
    return if (hours > 0) {
        String.format(Locale.ROOT, "%d:%02d:%02d", hours, minutes, seconds)
    } else {
        String.format(Locale.ROOT, "%02d:%02d", minutes, seconds)
    }
}
