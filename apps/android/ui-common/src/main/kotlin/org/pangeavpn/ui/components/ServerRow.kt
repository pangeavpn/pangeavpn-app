package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateError

private val RowShape = RoundedCornerShape(14.dp)

/** Converts a 2-letter ISO country code to its regional-indicator flag emoji. */
fun flagEmoji(country: String): String {
    val code = country.trim().uppercase()
    if (code.length != 2 || code[0] !in 'A'..'Z' || code[1] !in 'A'..'Z') return "🌐"
    val base = 0x1F1E6
    val first = base + (code[0] - 'A')
    val second = base + (code[1] - 'A')
    return String(Character.toChars(first)) + String(Character.toChars(second))
}

/**
 * One region in the picker: flag, name over its node line, load, and a tick when
 * it is the one in use. Ports .region-row from the desktop stylesheet.
 */
@Composable
fun ServerRow(
    name: String,
    country: String,
    load: Int?,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RowShape)
            .background(
                if (selected) MaterialTheme.colorScheme.primary.copy(alpha = 0.15f)
                else MaterialTheme.colorScheme.surface,
            )
            .border(
                width = if (focused) 2.dp else 1.dp,
                color = when {
                    focused || selected -> MaterialTheme.colorScheme.primary
                    else -> MaterialTheme.colorScheme.outlineVariant
                },
                shape = RowShape,
            )
            .onFocusChanged { focused = it.isFocused }
            .clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(text = flagEmoji(country), fontSize = 22.sp)

        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = name,
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (subtitle != null) {
                Text(
                    text = subtitle,
                    modifier = Modifier.padding(top = 1.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 11.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        if (load != null) {
            LoadIndicator(load = load)
        }

        // Always laid out so the rows either side of a selection stay aligned.
        Text(
            text = "✓",
            modifier = Modifier.width(18.dp),
            color = if (selected) MaterialTheme.colorScheme.primary else Color.Transparent,
            fontWeight = FontWeight.Bold,
            textAlign = TextAlign.Center,
        )
    }
}

/** Load bar plus its percentage — green under 40, amber under 75, red above. */
@Composable
private fun LoadIndicator(load: Int, modifier: Modifier = Modifier) {
    val pct = load.coerceIn(0, 100)
    val fillColor = when {
        pct < 40 -> StateConnected
        pct < 75 -> StateConnecting
        else -> StateError
    }
    Row(
        modifier = modifier,
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(
            modifier = Modifier
                .width(42.dp)
                .height(5.dp)
                .clip(RoundedCornerShape(3.dp))
                .background(MaterialTheme.colorScheme.outline),
        ) {
            Box(
                modifier = Modifier
                    .fillMaxHeight()
                    .fillMaxWidth(pct / 100f)
                    .clip(RoundedCornerShape(3.dp))
                    .background(fillColor),
            )
        }
        Text(
            text = "$pct%",
            modifier = Modifier.widthIn(min = 34.dp),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            fontSize = 13.sp,
            textAlign = TextAlign.End,
        )
    }
}
