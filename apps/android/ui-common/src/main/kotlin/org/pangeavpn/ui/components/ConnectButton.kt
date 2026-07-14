package org.pangeavpn.ui.components

import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.BrandOrange
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateError

/** Big circular connect/disconnect control. Ring color and label follow [state]. */
@Composable
fun ConnectButton(
    state: ConnState,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val ringColor = when (state) {
        ConnState.CONNECTED -> StateConnected
        ConnState.CONNECTING, ConnState.DISCONNECTING -> StateConnecting
        ConnState.ERROR -> StateError
        ConnState.DISCONNECTED -> BrandOrange
    }

    val actionLabel = when (state) {
        ConnState.CONNECTED -> stringResource(R.string.hero_disconnect)
        ConnState.CONNECTING, ConnState.DISCONNECTING -> stringResource(R.string.hero_provisioning)
        ConnState.DISCONNECTED, ConnState.ERROR -> stringResource(R.string.hero_connect)
    }

    val stateLabel = when (state) {
        ConnState.CONNECTED -> stringResource(R.string.state_connected)
        ConnState.CONNECTING -> stringResource(R.string.state_connecting)
        ConnState.DISCONNECTING -> stringResource(R.string.state_disconnecting)
        ConnState.ERROR -> stringResource(R.string.state_error)
        ConnState.DISCONNECTED -> stringResource(R.string.state_disconnected)
    }

    val transitioning = state == ConnState.CONNECTING || state == ConnState.DISCONNECTING

    val infiniteTransition = rememberInfiniteTransition(label = "connect-ring")
    val ringAlpha = if (transitioning) {
        infiniteTransition.animateFloat(
            initialValue = 0.35f,
            targetValue = 1f,
            animationSpec = infiniteRepeatable(
                animation = tween(durationMillis = 900, easing = FastOutSlowInEasing),
                repeatMode = RepeatMode.Reverse,
            ),
            label = "connect-ring-alpha",
        ).value
    } else {
        1f
    }

    // Visible focus ring for D-pad/keyboard navigation (TV), independent of touch ripple.
    var focused by remember { mutableStateOf(false) }

    Box(
        modifier = modifier
            .size(176.dp)
            .clip(CircleShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(
                width = if (focused) 4.dp else 3.dp,
                color = if (focused) MaterialTheme.colorScheme.onSurface else ringColor.copy(alpha = ringAlpha),
                shape = CircleShape,
            )
            .onFocusChanged { focused = it.isFocused }
            .clickable(enabled = !transitioning, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(
                text = stateLabel,
                style = MaterialTheme.typography.labelSmall,
                color = ringColor,
                letterSpacing = 1.5.sp,
            )
            Spacer(modifier = Modifier.height(6.dp))
            Text(
                text = actionLabel,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
                textAlign = TextAlign.Center,
            )
        }
    }
}
