package org.pangeavpn.ui.components

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.R

/** Brand mark, version, and the icon controls — the desktop header, unchanged.
 *  [onMenu] is optional because sign-in has nothing behind the burger yet. */
@Composable
fun PangeaHeader(
    version: String,
    onToggleTheme: () -> Unit,
    onMenu: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .height(56.dp)
            .padding(horizontal = 18.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        BrandLogo()
        Text(
            text = version,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            fontSize = 11.sp,
        )
        Spacer(Modifier.weight(1f))
        IconControl(
            contentDescription = stringResource(R.string.header_toggle_theme),
            onClick = onToggleTheme,
        ) { color -> drawSun(color) }
        if (onMenu != null) {
            IconControl(
                contentDescription = stringResource(R.string.header_menu),
                onClick = onMenu,
            ) { color -> drawBurger(color) }
        }
    }
}

@Composable
private fun IconControl(
    contentDescription: String,
    onClick: () -> Unit,
    draw: DrawScope.(Color) -> Unit,
) {
    val color = MaterialTheme.colorScheme.onSurfaceVariant
    Box(
        modifier = Modifier
            .size(40.dp)
            .clip(CircleShape)
            .clickable(role = Role.Button, onClickLabel = contentDescription, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(modifier = Modifier.size(19.dp)) { draw(color) }
    }
}

private fun DrawScope.drawSun(color: Color) {
    val stroke = size.width * 0.1f
    drawCircle(color, radius = size.width * 0.26f, style = Stroke(width = stroke))
    repeat(8) { index ->
        val angle = Math.toRadians(index * 45.0)
        val inner = size.width * 0.4f
        val outer = size.width * 0.5f
        val cx = size.width / 2
        val cy = size.height / 2
        drawLine(
            color = color,
            start = Offset(cx + (Math.cos(angle) * inner).toFloat(), cy + (Math.sin(angle) * inner).toFloat()),
            end = Offset(cx + (Math.cos(angle) * outer).toFloat(), cy + (Math.sin(angle) * outer).toFloat()),
            strokeWidth = stroke,
            cap = StrokeCap.Round,
        )
    }
}

private fun DrawScope.drawBurger(color: Color) {
    val stroke = size.height * 0.11f
    listOf(0.22f, 0.5f, 0.78f).forEach { fraction ->
        drawLine(
            color = color,
            start = Offset(0f, size.height * fraction),
            end = Offset(size.width, size.height * fraction),
            strokeWidth = stroke,
            cap = StrokeCap.Round,
        )
    }
}
