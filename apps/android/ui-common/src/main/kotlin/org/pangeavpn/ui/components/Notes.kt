package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateError

enum class NoteTone { INFO, WARNING, ERROR }

private fun NoteTone.icon(): ImageVector = when (this) {
    NoteTone.INFO -> Icons.Outlined.Info
    NoteTone.WARNING -> Icons.Outlined.Warning
    NoteTone.ERROR -> Icons.Outlined.ErrorOutline
}

@Composable
private fun NoteTone.tint(): Color = when (this) {
    NoteTone.INFO -> MaterialTheme.colorScheme.onSurfaceVariant
    NoteTone.WARNING -> StateConnecting
    NoteTone.ERROR -> StateError
}

/** A small line with an icon, for consequences, caveats and errors under a control. Pass icon
 *  to replace the tone's own glyph. An error is announced by TalkBack when it appears. */
@Composable
fun InfoNote(
    text: String,
    tone: NoteTone = NoteTone.INFO,
    icon: ImageVector? = null,
    modifier: Modifier = Modifier,
) {
    val tint = tone.tint()
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 4.dp)
            .then(
                if (tone == NoteTone.ERROR) {
                    Modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite }
                } else {
                    Modifier
                },
            ),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = icon ?: tone.icon(),
            contentDescription = null,
            modifier = Modifier.padding(top = 1.dp).size(14.dp),
            tint = tint,
        )
        Text(
            text = text,
            color = if (tone == NoteTone.WARNING) LocalTextSecondary.current else tint,
            fontSize = 12.sp,
            lineHeight = 16.sp,
        )
    }
}

/** The same note as a tinted, outlined card, for a callout that stands on its own. */
@Composable
fun NoteCard(
    text: String,
    tone: NoteTone = NoteTone.INFO,
    icon: ImageVector? = null,
    modifier: Modifier = Modifier,
) {
    val tint = tone.tint()
    val neutral = tone == NoteTone.INFO
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(PangeaCardShape)
            .background(if (neutral) MaterialTheme.colorScheme.surface else tint.copy(alpha = 0.1f))
            .border(
                width = 1.dp,
                color = if (neutral) MaterialTheme.colorScheme.outlineVariant else tint.copy(alpha = 0.35f),
                shape = PangeaCardShape,
            )
            .padding(horizontal = 14.dp, vertical = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = icon ?: tone.icon(),
            contentDescription = null,
            modifier = Modifier.padding(top = 1.dp).size(16.dp),
            tint = tint,
        )
        Text(
            text = text,
            color = if (tone == NoteTone.ERROR) StateError else LocalTextSecondary.current,
            fontSize = 13.sp,
            lineHeight = 18.sp,
        )
    }
}

private val TagShape = RoundedCornerShape(4.dp)

/** A small label beside a title: a washed pill ("This device", "In use" in green), or a
 *  squared caps tag when outlined ("ENTRY", "Pinned"). */
@Composable
fun PillBadge(
    text: String,
    color: Color = MaterialTheme.colorScheme.primary,
    outlined: Boolean = false,
    modifier: Modifier = Modifier,
) {
    if (outlined) {
        Text(
            text = text.uppercase(),
            modifier = modifier
                .border(width = 1.dp, color = color, shape = TagShape)
                .padding(horizontal = 5.dp, vertical = 2.dp),
            color = color,
            fontSize = 9.sp,
            lineHeight = 12.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.sp,
            maxLines = 1,
        )
    } else {
        Text(
            text = text,
            modifier = modifier
                .clip(PillShape)
                .background(color.copy(alpha = 0.14f))
                .padding(horizontal = 8.dp, vertical = 3.dp),
            color = color,
            fontSize = 11.sp,
            lineHeight = 14.sp,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
        )
    }
}
