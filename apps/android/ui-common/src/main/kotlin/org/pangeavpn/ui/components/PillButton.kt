package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
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
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.theme.StateError

/** Pill radius large enough to always round fully, matching border-radius: 999px. */
internal val PillShape = RoundedCornerShape(percent = 50)

/** The filled pill for a screen's main move: accent, or red when destructive. Full width unless
 *  compact. Disabled goes neutral, since a faded accent still reads as live; loading keeps its colour. */
@Composable
fun PillButton(
    label: String,
    onClick: () -> Unit,
    enabled: Boolean = true,
    loading: Boolean = false,
    destructive: Boolean = false,
    compact: Boolean = false,
    modifier: Modifier = Modifier,
) {
    val muted = !enabled && !loading
    Pill(
        label = label,
        onClick = onClick,
        enabled = enabled,
        loading = loading,
        compact = compact,
        primary = true,
        fill = when {
            muted -> MaterialTheme.colorScheme.surface
            destructive -> StateError
            else -> MaterialTheme.colorScheme.primary
        },
        content = if (muted) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onPrimary,
        outline = if (muted) MaterialTheme.colorScheme.outline else Color.Transparent,
        focusOutline = MaterialTheme.colorScheme.onSurface,
        modifier = modifier,
    )
}

/** The pill in outline, for the actions that are not the screen's main move. A tint (accent, or
 *  red when destructive) colours the label and the outline. Full width unless compact. */
@Composable
fun GhostButton(
    label: String,
    onClick: () -> Unit,
    enabled: Boolean = true,
    loading: Boolean = false,
    destructive: Boolean = false,
    compact: Boolean = false,
    accent: Color? = null,
    modifier: Modifier = Modifier,
) {
    val tint = if (destructive) StateError else accent
    Pill(
        label = label,
        onClick = onClick,
        enabled = enabled,
        loading = loading,
        compact = compact,
        primary = false,
        fill = Color.Transparent,
        content = tint ?: MaterialTheme.colorScheme.onSurface,
        outline = tint?.copy(alpha = 0.55f) ?: MaterialTheme.colorScheme.outline,
        focusOutline = MaterialTheme.colorScheme.primary,
        modifier = modifier.alpha(if (enabled || loading) 1f else 0.4f),
    )
}

@Composable
private fun Pill(
    label: String,
    onClick: () -> Unit,
    enabled: Boolean,
    loading: Boolean,
    compact: Boolean,
    primary: Boolean,
    fill: Color,
    content: Color,
    outline: Color,
    focusOutline: Color,
    modifier: Modifier,
) {
    var focused by remember { mutableStateOf(false) }

    Box(
        modifier = modifier
            .then(if (compact) Modifier else Modifier.fillMaxWidth())
            .heightIn(min = 48.dp)
            .clip(PillShape)
            .background(fill)
            .border(
                width = if (focused) 2.dp else 1.dp,
                color = if (focused) focusOutline else outline,
                shape = PillShape,
            )
            .onFocusChanged { focused = it.isFocused }
            // Still focusable while loading: disabling it would drop D-pad focus on a television.
            .clickable(enabled = enabled, role = Role.Button) { if (!loading) onClick() }
            .padding(
                horizontal = if (compact) 18.dp else 20.dp,
                vertical = if (compact) 8.dp else if (primary) 16.dp else 14.dp,
            ),
        contentAlignment = Alignment.Center,
    ) {
        // Kept in the layout while loading, so the pill holds its width and TalkBack its name.
        Text(
            text = label,
            modifier = Modifier.alpha(if (loading) 0f else 1f),
            color = content,
            fontSize = if (compact) 14.sp else if (primary) 17.sp else 15.sp,
            fontWeight = FontWeight.SemiBold,
            textAlign = TextAlign.Center,
            maxLines = if (compact) 1 else 2,
            overflow = TextOverflow.Ellipsis,
        )
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(if (compact) 18.dp else 22.dp),
                color = content,
                strokeWidth = 2.dp,
            )
        }
    }
}

/** A 48 dp round icon button; label is what TalkBack reads. */
@Composable
fun IconAction(
    icon: ImageVector,
    label: String,
    onClick: () -> Unit,
    enabled: Boolean = true,
    tint: Color = MaterialTheme.colorScheme.onSurfaceVariant,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }

    Box(
        modifier = modifier
            .size(48.dp)
            .alpha(if (enabled) 1f else 0.4f)
            .clip(CircleShape)
            .border(
                width = 2.dp,
                color = if (focused) MaterialTheme.colorScheme.primary else Color.Transparent,
                shape = CircleShape,
            )
            .onFocusChanged { focused = it.isFocused }
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = label,
            modifier = Modifier.size(22.dp),
            tint = tint,
        )
    }
}
