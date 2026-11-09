package org.pangeavpn.ui.components

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.List
import androidx.compose.material.icons.rounded.SwapHoriz
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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary

/** The door to the full list, dashed so it reads as a way through, with the rotate button beside it. */
@Composable
fun AllRegionsRow(
    moreCount: Int,
    canRotate: Boolean,
    onOpen: () -> Unit,
    onRotate: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(IntrinsicSize.Min),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        OpenButton(moreCount, onOpen, Modifier.weight(1f).fillMaxHeight())
        RotateButton(canRotate, onRotate, Modifier.fillMaxHeight())
    }
}

@Composable
private fun OpenButton(moreCount: Int, onOpen: () -> Unit, modifier: Modifier) {
    var focused by remember { mutableStateOf(false) }
    val outline = if (focused) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outline
    val text = LocalTextSecondary.current

    Row(
        modifier = modifier
            .heightIn(min = 48.dp)
            .clip(PangeaCardShape)
            .drawBehind {
                val stroke = (if (focused) 2.dp else 1.dp).toPx()
                drawRoundRect(
                    color = outline,
                    topLeft = Offset(stroke / 2, stroke / 2),
                    size = Size(size.width - stroke, size.height - stroke),
                    cornerRadius = CornerRadius(14.dp.toPx()),
                    style = Stroke(
                        width = stroke,
                        pathEffect = PathEffect.dashPathEffect(floatArrayOf(4.dp.toPx(), 4.dp.toPx())),
                    ),
                )
            }
            .onFocusChanged { focused = it.isFocused }
            .clickable(role = Role.Button, onClick = onOpen)
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            imageVector = Icons.AutoMirrored.Rounded.List,
            contentDescription = null,
            modifier = Modifier.size(20.dp),
            tint = text,
        )
        Text(
            text = stringResource(R.string.hero_all_regions),
            modifier = Modifier.weight(1f),
            color = text,
            fontSize = 15.sp,
            lineHeight = 20.sp,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (moreCount > 0) {
            Text(
                text = stringResource(R.string.region_more, moreCount.toString()),
                color = text,
                fontFamily = FontFamily.Monospace,
                fontSize = 12.sp,
                lineHeight = 16.sp,
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun RotateButton(canRotate: Boolean, onRotate: () -> Unit, modifier: Modifier) {
    var focused by remember { mutableStateOf(false) }
    val label = stringResource(R.string.main_rotate)

    Box(
        modifier = modifier
            .width(48.dp)
            .heightIn(min = 48.dp)
            .alpha(if (canRotate) 1f else 0.4f)
            .cardSurface(focused = focused)
            .onFocusChanged { focused = it.isFocused }
            .clickable(enabled = canRotate, role = Role.Button, onClick = onRotate)
            .semantics { contentDescription = label },
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = Icons.Rounded.SwapHoriz,
            contentDescription = null,
            modifier = Modifier.size(20.dp),
            tint = LocalTextSecondary.current,
        )
    }
}
