package org.pangeavpn.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowRight
import androidx.compose.material.icons.rounded.Lock
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.disabled
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.connection.MainAction
import org.pangeavpn.ui.R
import org.pangeavpn.ui.text.mainActionText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.reducedMotion

/** The action bar's control. LOCKDOWN draws the Lockdown note, whose tap is [onClick]; HIDDEN draws nothing. */
@Composable
fun MainActionButton(action: MainAction, onClick: () -> Unit, modifier: Modifier = Modifier) {
    when (action) {
        MainAction.HIDDEN -> Unit
        MainAction.LOCKDOWN -> LockdownNote(onClick, modifier)
        else -> ActionPill(action, onClick, modifier)
    }
}

@Composable
private fun ActionPill(action: MainAction, onClick: () -> Unit, modifier: Modifier) {
    val reduced = reducedMotion()
    val pulsing = action == MainAction.PROVISIONING || action == MainAction.DISCONNECTING
    val enabled = action == MainAction.CONNECT || action == MainAction.STOP || action == MainAction.DISCONNECT
    // Leaving is normal, so the ways out stay quiet.
    val primary = action == MainAction.CONNECT || action == MainAction.PROVISIONING

    val fill by animateColorAsState(
        if (primary) MaterialTheme.colorScheme.primary else Color.Transparent, Motion.state(reduced), label = "action-fill",
    )
    val content by animateColorAsState(
        if (primary) MaterialTheme.colorScheme.onPrimary else LocalTextSecondary.current, Motion.state(reduced),
        label = "action-content",
    )
    val outline by animateColorAsState(
        if (primary) Color.Transparent else MaterialTheme.colorScheme.outline, Motion.state(reduced),
        label = "action-outline",
    )
    val pulse = remember { Animatable(1f) }
    LaunchedEffect(pulsing, reduced) {
        when {
            !pulsing -> pulse.animateTo(1f, Motion.quick())
            reduced -> pulse.snapTo(0.75f)
            else -> {
                pulse.snapTo(1f)
                pulse.animateTo(0.55f, infiniteRepeatable(tween(900, easing = FastOutSlowInEasing), RepeatMode.Reverse))
            }
        }
    }
    var focused by remember { mutableStateOf(false) }
    val haptics = LocalHapticFeedback.current

    Box(
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .graphicsLayer { alpha = pulse.value }
            .clip(PillShape)
            .background(fill)
            .border(
                width = if (focused) 2.dp else 1.dp,
                color = if (focused) MaterialTheme.colorScheme.onSurface else outline,
                shape = PillShape,
            )
            .onFocusChanged { focused = it.isFocused }
            // Never disabled outright, so D-pad focus stays put on a television while it works.
            .clickable(role = Role.Button) {
                if (enabled) {
                    haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                    onClick()
                }
            }
            .semantics { if (!enabled) disabled() }
            .padding(horizontal = 20.dp, vertical = 14.dp),
        contentAlignment = Alignment.Center,
    ) {
        FadeSwap(mainActionText(action).orEmpty()) { label ->
            Text(
                text = label,
                color = content,
                fontSize = 17.sp,
                lineHeight = 22.sp,
                fontWeight = FontWeight.SemiBold,
                textAlign = TextAlign.Center,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

// Lockdown hides Disconnect; the note says why and leads to the switch that turns it off.
@Composable
private fun LockdownNote(onClick: () -> Unit, modifier: Modifier) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 52.dp)
            .clip(PillShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(
                width = if (focused) 2.dp else 1.dp,
                color = if (focused) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outline,
                shape = PillShape,
            )
            .onFocusChanged { focused = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            imageVector = Icons.Rounded.Lock,
            contentDescription = null,
            modifier = Modifier.size(18.dp),
            tint = MaterialTheme.colorScheme.primary,
        )
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(1.dp)) {
            Text(
                text = stringResource(R.string.main_lockdown_note),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 15.sp,
                lineHeight = 20.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = stringResource(R.string.main_lockdown_hint),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 12.sp,
                lineHeight = 16.sp,
            )
        }
        Icon(
            imageVector = Icons.AutoMirrored.Rounded.KeyboardArrowRight,
            contentDescription = null,
            modifier = Modifier.size(18.dp),
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
