package org.pangeavpn.ui.components

import android.content.res.Configuration
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.PhoneAndroid
import androidx.compose.material.icons.rounded.TabletAndroid
import androidx.compose.material.icons.rounded.Tv
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.SubcomposeLayout
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlin.math.min
import org.pangeavpn.core.servers.RouteNode
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.reducedMotion

private const val ROUTE_CYCLE_MS = 3_200

/** You, then the entry, then the exit; names drop to flags alone when the row is too narrow. */
@Composable
fun RoutePath(entry: RouteNode, exit: RouteNode, connected: Boolean, modifier: Modifier = Modifier) {
    val spoken = stringResource(R.string.main_route_label, entry.name, exit.name)
    val phase = if (connected && !reducedMotion()) routePhase() else null

    SubcomposeLayout(
        modifier = modifier
            .fillMaxWidth()
            .clearAndSetSemantics { contentDescription = spoken },
    ) { constraints ->
        val loose = constraints.copy(minWidth = 0, minHeight = 0, maxWidth = Constraints.Infinity)
        var strip = subcompose(true) { RouteStrip(entry, exit, connected, phase, named = true) }.first().measure(loose)
        if (constraints.hasBoundedWidth && strip.width > constraints.maxWidth) {
            strip = subcompose(false) { RouteStrip(entry, exit, connected, phase, named = false) }
                .first()
                .measure(loose.copy(maxWidth = constraints.maxWidth))
        }
        val width = if (constraints.hasBoundedWidth) constraints.maxWidth else strip.width
        layout(width, strip.height) { strip.placeRelative(0, 0) }
    }
}

@Composable
private fun routePhase(): State<Float> = rememberInfiniteTransition(label = "route").animateFloat(
    initialValue = 0f,
    targetValue = 1f,
    animationSpec = infiniteRepeatable(tween(ROUTE_CYCLE_MS, easing = LinearEasing)),
    label = "route-phase",
)

@Composable
private fun RouteStrip(entry: RouteNode, exit: RouteNode, live: Boolean, phase: State<Float>?, named: Boolean) {
    val accent = MaterialTheme.colorScheme.primary
    val idleRing = MaterialTheme.colorScheme.outline
    Row(
        modifier = Modifier
            .clip(PillShape)
            .background(Brush.linearGradient(listOf(accent.copy(alpha = 0.14f), accent.copy(alpha = 0.03f))))
            .border(1.dp, accent.copy(alpha = 0.28f), PillShape)
            .padding(3.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        DeviceStop()
        RouteLink(live, phase, offset = 0f)
        RouteStop(entry, named, ring = if (live) accent.copy(alpha = 0.7f) else idleRing, glow = false)
        RouteLink(live, phase, offset = 0.5f)
        RouteStop(exit, named, ring = if (live) StateConnected else idleRing, glow = live)
    }
}

@Composable
private fun DeviceStop() {
    val configuration = LocalConfiguration.current
    val icon = when {
        (configuration.uiMode and Configuration.UI_MODE_TYPE_MASK) == Configuration.UI_MODE_TYPE_TELEVISION ->
            Icons.Rounded.Tv
        configuration.smallestScreenWidthDp >= 600 -> Icons.Rounded.TabletAndroid
        else -> Icons.Rounded.PhoneAndroid
    }
    Box(
        modifier = Modifier
            .clip(PillShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(1.dp, MaterialTheme.colorScheme.outline, PillShape)
            .padding(horizontal = 7.dp, vertical = 4.dp),
    ) {
        Icon(imageVector = icon, contentDescription = null, modifier = Modifier.size(13.dp), tint = LocalTextSecondary.current)
    }
}

@Composable
private fun RouteStop(node: RouteNode, named: Boolean, ring: Color, glow: Boolean) {
    val glowColor = StateConnected.copy(alpha = 0.22f)
    Row(
        modifier = Modifier
            .drawBehind {
                if (glow) {
                    val stroke = 2.dp.toPx()
                    val inset = -stroke / 2
                    drawRoundRect(
                        color = glowColor,
                        topLeft = Offset(inset, inset),
                        size = Size(size.width + stroke, size.height + stroke),
                        cornerRadius = CornerRadius((size.height + stroke) / 2),
                        style = Stroke(stroke),
                    )
                }
            }
            .clip(PillShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(1.dp, ring, PillShape)
            .padding(start = 6.dp, end = if (named) 9.dp else 6.dp, top = 3.dp, bottom = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        Text(text = flagEmoji(node.country), fontSize = 13.sp, lineHeight = 17.sp)
        if (named) {
            Text(
                text = node.name,
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 12.sp,
                lineHeight = 16.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                softWrap = false,
            )
        }
    }
}

// One packet per cycle crosses the first link, then the second, as on desktop.
@Composable
private fun RouteLink(live: Boolean, phase: State<Float>?, offset: Float) {
    val accent = MaterialTheme.colorScheme.primary
    val dash = if (live) accent.copy(alpha = 0.9f) else MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.55f)
    val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl

    Canvas(modifier = Modifier.size(width = 22.dp, height = 8.dp)) {
        val mid = size.height / 2
        val step = 6.dp.toPx()
        val dashSize = Size(3.3.dp.toPx(), 2.dp.toPx())
        var x = 0f
        while (x < size.width) {
            drawRect(dash, topLeft = Offset(x, mid - dashSize.height / 2), size = dashSize)
            x += step
        }
        val local = ((phase?.value ?: return@Canvas) - offset + 1f) % 1f
        if (local >= 0.5f) return@Canvas
        val pulse = local / 0.5f
        val fade = min(1f, min(pulse, 1f - pulse) / 0.15f)
        val dot = 5.dp.toPx()
        val travel = pulse * (size.width - dot)
        val center = Offset((if (rtl) size.width - dot - travel else travel) + dot / 2, mid)
        drawCircle(accent.copy(alpha = 0.3f * fade), radius = dot / 2 + 2.dp.toPx(), center = center)
        drawCircle(accent.copy(alpha = fade), radius = dot / 2, center = center)
    }
}
