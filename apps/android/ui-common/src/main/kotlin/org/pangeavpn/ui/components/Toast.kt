package org.pangeavpn.ui.components

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.util.concurrent.atomic.AtomicLong
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateConnecting

enum class ToastStyle { INFO, SUCCESS, WARNING }

data class Toast(val id: Long, val text: String, val style: ToastStyle)

/** One message at a time for the whole process; a new one replaces the old so feedback is
 *  never stale. Safe to call from any thread, the service included. */
object ToastCenter {
    const val VISIBLE_MS = 4_000L

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val nextId = AtomicLong()

    private val _current = MutableStateFlow<Toast?>(null)
    val current: StateFlow<Toast?> = _current.asStateFlow()

    // A sheet or dialog is its own window with its own host; only the newest one draws.
    private val stack = MutableStateFlow<List<Any>>(emptyList())
    internal val hosts: StateFlow<List<Any>> = stack.asStateFlow()

    fun show(text: String, style: ToastStyle = ToastStyle.INFO) {
        val toast = Toast(nextId.incrementAndGet(), text, style)
        _current.value = toast
        scope.launch {
            delay(VISIBLE_MS)
            dismiss(toast.id)
        }
    }

    /** With an id, clears only that toast, so an old timer never removes a newer message. */
    fun dismiss(id: Long? = null) {
        _current.update { if (id == null || it?.id == id) null else it }
    }

    internal fun attach(host: Any) {
        stack.update { it - host + host }
    }

    internal fun detach(host: Any) {
        stack.update { it - host }
    }
}

/** Draws ToastCenter's message at the bottom of the enclosing Box, above the system bars and
 *  keyboard. Put one last in the root Box and one in each sheet's Box; only the newest draws. */
@Composable
fun BoxScope.ToastHost(modifier: Modifier = Modifier) {
    val host = remember { Any() }
    DisposableEffect(host) {
        ToastCenter.attach(host)
        onDispose { ToastCenter.detach(host) }
    }
    val hosts by ToastCenter.hosts.collectAsState()
    val current by ToastCenter.current.collectAsState()
    val shown = if (hosts.lastOrNull() === host) current else null

    Box(
        modifier = modifier
            .align(Alignment.BottomCenter)
            .fillMaxWidth()
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom))
            .padding(start = 16.dp, end = 16.dp, bottom = 12.dp),
        contentAlignment = Alignment.BottomCenter,
    ) {
        AnimatedContent(
            targetState = shown,
            transitionSpec = {
                (slideInVertically(Motion.quick()) { it } + fadeIn(Motion.quick())) togetherWith
                    (slideOutVertically(Motion.quick()) { it } + fadeOut(Motion.quick())) using
                    SizeTransform(clip = false)
            },
            contentAlignment = Alignment.BottomCenter,
            label = "toast",
        ) { toast ->
            if (toast != null) ToastBanner(toast)
        }
    }
}

@Composable
private fun ToastBanner(toast: Toast) {
    val tint = when (toast.style) {
        ToastStyle.INFO -> MaterialTheme.colorScheme.onSurface
        ToastStyle.SUCCESS -> StateConnected
        ToastStyle.WARNING -> StateConnecting
    }
    val icon = when (toast.style) {
        ToastStyle.INFO -> Icons.Outlined.Info
        ToastStyle.SUCCESS -> Icons.Outlined.CheckCircle
        ToastStyle.WARNING -> Icons.Outlined.Warning
    }

    Row(
        modifier = Modifier
            .widthIn(max = 560.dp)
            .fillMaxWidth()
            .shadow(elevation = 12.dp, shape = PangeaCardShape)
            .background(MaterialTheme.colorScheme.surfaceVariant)
            .border(width = 1.dp, color = MaterialTheme.colorScheme.outline, shape = PangeaCardShape)
            .clickable(onClickLabel = stringResource(R.string.settings_close)) { ToastCenter.dismiss(toast.id) }
            .semantics { liveRegion = LiveRegionMode.Polite }
            .padding(horizontal = 16.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.size(20.dp),
            tint = tint,
        )
        Text(
            text = toast.text,
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 14.sp,
            lineHeight = 20.sp,
            fontWeight = FontWeight.Medium,
        )
    }
}
