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
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary

/** Pill radius large enough to always round fully, matching border-radius: 999px. */
private val PillShape = RoundedCornerShape(percent = 50)

/**
 * The primary action. Accent-filled while there is something to connect to, and
 * quiet-outlined once connected, so disconnecting never looks like the main move.
 */
@Composable
fun ConnectButton(
    state: ConnState,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val busy = state == ConnState.CONNECTING || state == ConnState.DISCONNECTING
    val connected = state == ConnState.CONNECTED

    val label = when (state) {
        ConnState.CONNECTED -> stringResource(R.string.hero_disconnect)
        ConnState.CONNECTING -> stringResource(R.string.hero_provisioning)
        ConnState.DISCONNECTING -> stringResource(R.string.hero_disconnecting)
        ConnState.DISCONNECTED, ConnState.ERROR -> stringResource(R.string.hero_connect)
    }

    val pulse by rememberInfiniteTransition(label = "connect-pulse").animateFloat(
        initialValue = 0.55f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 900, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "connect-pulse-alpha",
    )

    var focused by remember { mutableStateOf(false) }

    Box(
        modifier = modifier
            .fillMaxWidth()
            .alpha(if (busy) pulse else 1f)
            .clip(PillShape)
            .background(if (connected) Color.Transparent else MaterialTheme.colorScheme.primary)
            .border(
                width = if (focused) 2.dp else 1.dp,
                color = when {
                    focused -> MaterialTheme.colorScheme.onSurface
                    connected -> MaterialTheme.colorScheme.outline
                    else -> Color.Transparent
                },
                shape = PillShape,
            )
            .onFocusChanged { focused = it.isFocused }
            .clickable(enabled = !busy, role = Role.Button, onClick = onClick)
            .padding(vertical = 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (connected) LocalTextSecondary.current else MaterialTheme.colorScheme.onPrimary,
            fontSize = 17.sp,
            fontWeight = FontWeight.SemiBold,
        )
    }
}
