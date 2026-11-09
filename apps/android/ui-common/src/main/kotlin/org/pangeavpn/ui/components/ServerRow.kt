package org.pangeavpn.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.selection.selectable
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
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateError

private const val GLOBE = "🌐"

/** A 2-letter ISO country code as its regional-indicator flag; a globe for anything else. */
fun flagEmoji(country: String): String {
    val code = country.trim().uppercase()
    if (code.length != 2 || code[0] !in 'A'..'Z' || code[1] !in 'A'..'Z') return GLOBE
    val base = 0x1F1E6
    return String(Character.toChars(base + (code[0] - 'A'))) + String(Character.toChars(base + (code[1] - 'A')))
}

/** One region or node: flag, name over its monospaced line, load. The selection is the accent
 *  border and wash, spoken as selected; a disabled row dims to 60%. */
@Composable
fun ServerRow(
    name: String,
    country: String,
    subtitle: String?,
    load: Int?,
    selected: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
    badge: String? = null,
    trailing: (@Composable () -> Unit)? = null,
) {
    var focused by remember { mutableStateOf(false) }

    Row(
        modifier = modifier
            .fillMaxWidth()
            .alpha(if (enabled) 1f else 0.6f)
            .cardSurface(selected = selected, focused = focused)
            .onFocusChanged { focused = it.isFocused }
            .selectable(selected = selected, enabled = enabled, role = Role.Button, onClick = onClick)
            .heightIn(min = 54.dp)
            .padding(horizontal = 14.dp, vertical = 9.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = flagEmoji(country),
            modifier = Modifier.clearAndSetSemantics {},
            fontSize = 24.sp,
            lineHeight = 30.sp,
        )
        Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    text = name,
                    modifier = Modifier.weight(1f, fill = false),
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 16.sp,
                    lineHeight = 21.sp,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (badge != null) PillBadge(text = badge, outlined = true)
            }
            if (!subtitle.isNullOrEmpty()) {
                Text(
                    text = subtitle,
                    color = LocalTextSecondary.current,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 12.sp,
                    lineHeight = 16.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (load != null) LoadMeter(load)
        trailing?.invoke()
    }
}

/** Load bar plus its percentage, spoken as one "Server load N%". */
@Composable
fun LoadMeter(load: Int, modifier: Modifier = Modifier) {
    val pct = load.coerceIn(0, 100)
    val spoken = stringResource(R.string.regions_load, "$pct%")
    Row(
        modifier = modifier.clearAndSetSemantics { contentDescription = spoken },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(
            modifier = Modifier
                .size(width = 42.dp, height = 5.dp)
                .clip(PillShape)
                .background(MaterialTheme.colorScheme.outline),
        ) {
            Box(
                modifier = Modifier
                    .fillMaxHeight()
                    .fillMaxWidth(pct / 100f)
                    .clip(PillShape)
                    .background(loadTone(pct)),
            )
        }
        Text(
            text = "$pct%",
            modifier = Modifier.widthIn(min = 36.dp),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            fontFamily = FontFamily.Monospace,
            fontSize = 13.sp,
            lineHeight = 16.sp,
            textAlign = TextAlign.End,
            maxLines = 1,
        )
    }
}

private fun loadTone(pct: Int): Color = when {
    pct < 40 -> StateConnected
    pct < 75 -> StateConnecting
    else -> StateError
}
