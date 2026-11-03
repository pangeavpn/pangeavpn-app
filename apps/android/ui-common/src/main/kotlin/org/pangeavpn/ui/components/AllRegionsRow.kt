package org.pangeavpn.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary

private val RowShape = RoundedCornerShape(14.dp)

/**
 * The door to the full region list, with the refresh control beside it. Dashed
 * rather than solid so it reads as a way through rather than another choice.
 */
@Composable
fun AllRegionsRow(
    moreCount: Int,
    onOpen: () -> Unit,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }
    val outline = if (focused) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outline
    val label = if (focused) MaterialTheme.colorScheme.primary else LocalTextSecondary.current

    Row(
        modifier = modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(
            modifier = Modifier
                .weight(1f)
                .clip(RowShape)
                .drawBehind {
                    drawRoundRect(
                        color = outline,
                        style = Stroke(
                            width = 1.dp.toPx(),
                            pathEffect = PathEffect.dashPathEffect(
                                floatArrayOf(4.dp.toPx(), 4.dp.toPx()),
                            ),
                        ),
                        cornerRadius = CornerRadius(14.dp.toPx()),
                    )
                }
                .onFocusChanged { focused = it.isFocused }
                .clickable(role = Role.Button, onClick = onOpen)
                .padding(horizontal = 14.dp, vertical = 13.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            ListGlyph(modifier = Modifier.size(18.dp))
            Text(
                text = stringResource(R.string.hero_all_regions),
                modifier = Modifier.weight(1f),
                color = label,
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
            )
            if (moreCount > 0) {
                Text(
                    text = stringResource(R.string.region_more, moreCount.toString()),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 11.sp,
                )
            }
        }

        Box(
            modifier = Modifier
                .size(44.dp)
                .clip(CircleShape)
                .clickable(role = Role.Button, onClick = onRefresh),
            contentAlignment = Alignment.Center,
        ) {
            RefreshGlyph(modifier = Modifier.size(18.dp))
        }
    }
}

@Composable
private fun ListGlyph(modifier: Modifier = Modifier) {
    val color = LocalTextSecondary.current
    Canvas(modifier = modifier) {
        val rows = 3
        val gap = size.height / (rows - 0.5f)
        repeat(rows) { index ->
            val y = gap * index + gap * 0.25f
            drawCircle(color, radius = size.width * 0.055f, center = Offset(size.width * 0.06f, y))
            drawLine(
                color = color,
                start = Offset(size.width * 0.28f, y),
                end = Offset(size.width, y),
                strokeWidth = size.width * 0.09f,
                cap = StrokeCap.Round,
            )
        }
    }
}

@Composable
private fun RefreshGlyph(modifier: Modifier = Modifier) {
    val color = MaterialTheme.colorScheme.onSurfaceVariant
    Canvas(modifier = modifier) {
        val stroke = Stroke(width = size.width * 0.11f, cap = StrokeCap.Round)
        val inset = stroke.width
        drawArc(
            color = color,
            startAngle = -40f,
            sweepAngle = 300f,
            useCenter = false,
            topLeft = Offset(inset, inset),
            size = Size(size.width - inset * 2, size.height - inset * 2),
            style = stroke,
        )
        // The open end gets an arrow head so the direction of travel is obvious.
        val tip = Offset(size.width * 0.9f, size.height * 0.18f)
        drawLine(color, tip, Offset(tip.x, tip.y + size.height * 0.3f), stroke.width, StrokeCap.Round)
        drawLine(color, tip, Offset(tip.x - size.width * 0.3f, tip.y), stroke.width, StrokeCap.Round)
    }
}
