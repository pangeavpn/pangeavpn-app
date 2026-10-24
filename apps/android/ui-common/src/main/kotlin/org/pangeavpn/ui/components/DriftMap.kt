package org.pangeavpn.ui.components

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.core.updateTransition
import androidx.compose.animation.animateColor
import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.DrawScope
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.vector.PathParser
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateError

/** The drift map is authored in an 800x800 space, like the desktop SVG's viewBox. */
private const val VIEWPORT = 800f

/** How far each plate pulls away when the world is apart, in viewport units.
 *  Mirrors the --dx/--dy/--dr custom properties in styles.css. */
private data class Drift(val dx: Float, val dy: Float, val degrees: Float)

private val NORTH_DRIFT = Drift(20.8f, -88.4f, -3.2f)
private val SOUTH_WEST_DRIFT = Drift(-78f, 57.2f, -4f)
private val SOUTH_EAST_DRIFT = Drift(83.2f, 67.6f, 4.8f)

private const val NORTH_RIDGE = "M 296 220 C 330 208, 366 224, 400 212 C 424 204, 448 214, 470 226"
private const val SOUTH_WEST_RIDGE = "M 268 440 C 296 464, 306 500, 334 524 C 356 542, 382 556, 400 574"

private val NORTH_STIPPLE = listOf(
    330f to 180f, 392f to 158f, 450f to 180f, 520f to 230f,
    280f to 260f, 300f to 310f, 560f to 200f,
)
private val SOUTH_WEST_STIPPLE = listOf(
    300f to 470f, 260f to 520f, 340f to 560f, 310f to 610f, 270f to 410f,
)
private val SOUTH_EAST_STIPPLE = listOf(
    560f to 470f, 540f to 560f, 530f to 610f, 590f to 505f,
)

private class Plate(val outline: Path, val ridge: Path?, val stipple: List<Pair<Float, Float>>, val drift: Drift)

private fun parse(data: String): Path = PathParser().parsePathString(data).toPath()

/**
 * The continental-drift map behind the hero. The plates sit together while the
 * tunnel is up and pull apart when it is down, which is the whole metaphor the
 * headline leans on.
 */
@Composable
fun DriftMap(state: ConnState, modifier: Modifier = Modifier) {
    val plates = remember {
        listOf(
            Plate(parse(DRIFT_NORTH), parse(NORTH_RIDGE), NORTH_STIPPLE, NORTH_DRIFT),
            Plate(parse(DRIFT_SOUTH_WEST), parse(SOUTH_WEST_RIDGE), SOUTH_WEST_STIPPLE, SOUTH_WEST_DRIFT),
            Plate(parse(DRIFT_SOUTH_EAST), null, SOUTH_EAST_STIPPLE, SOUTH_EAST_DRIFT),
        )
    }

    val transition = updateTransition(targetState = state, label = "drift")
    val spread by transition.animateFloat(
        transitionSpec = { tween(durationMillis = 1400) },
        label = "drift-spread",
    ) { it.spread() }
    val tint by transition.animateColor(
        transitionSpec = { tween(durationMillis = 700) },
        label = "drift-tint",
    ) { it.tint() }
    val alpha by transition.animateFloat(
        transitionSpec = { tween(durationMillis = 700) },
        label = "drift-alpha",
    ) { it.mapAlpha() }

    val orbit by rememberInfiniteTransition(label = "drift-orbit").animateFloat(
        initialValue = 0f,
        targetValue = 360f,
        animationSpec = infiniteRepeatable(tween(durationMillis = 90_000, easing = LinearEasing)),
        label = "drift-orbit-angle",
    )

    Canvas(modifier = modifier) {
        val unit = size.minDimension / VIEWPORT
        scale(scale = unit, pivot = Offset.Zero) {
            drawGraticule(tint, alpha)
            drawOrbit(tint, alpha, orbit)
            plates.forEach { drawPlate(it, tint, alpha, spread) }
            drawCompassRose(tint, alpha)
        }
    }
}

/** Fully together when connected, barely moved while connecting, apart otherwise. */
private fun ConnState.spread(): Float = when (this) {
    ConnState.CONNECTED -> 0f
    ConnState.CONNECTING -> 0.12f
    ConnState.DISCONNECTED, ConnState.DISCONNECTING, ConnState.ERROR -> 1f
}

private fun ConnState.tint(): Color = when (this) {
    ConnState.CONNECTED -> StateConnected
    ConnState.ERROR -> StateError
    ConnState.CONNECTING -> DriftAccent
    ConnState.DISCONNECTED, ConnState.DISCONNECTING -> DriftIdle
}

private fun ConnState.mapAlpha(): Float = when (this) {
    ConnState.CONNECTED -> 1f
    ConnState.CONNECTING -> 0.85f
    ConnState.ERROR -> 0.55f
    ConnState.DISCONNECTED, ConnState.DISCONNECTING -> 0.5f
}

private val DriftAccent = Color(0xFFC3562B)
private val DriftIdle = Color(0xFF84847C)

private fun DrawScope.drawGraticule(tint: Color, alpha: Float) {
    val stroke = Stroke(width = 0.75f)
    val paint = tint.copy(alpha = 0.16f * alpha)
    drawCircleOutline(400f, 400f, 340f, paint, stroke)
    drawEllipseOutline(240f, 340f, paint, stroke)
    drawEllipseOutline(120f, 340f, paint, stroke)
    drawEllipseOutline(340f, 240f, paint, stroke)
    drawEllipseOutline(340f, 120f, paint, stroke)
    drawLine(paint, Offset(60f, 400f), Offset(740f, 400f), strokeWidth = 0.75f)
    drawLine(paint, Offset(400f, 60f), Offset(400f, 740f), strokeWidth = 0.75f)
}

private fun DrawScope.drawCircleOutline(cx: Float, cy: Float, r: Float, color: Color, stroke: Stroke) {
    drawCircle(color = color, radius = r, center = Offset(cx, cy), style = stroke)
}

private fun DrawScope.drawEllipseOutline(rx: Float, ry: Float, color: Color, stroke: Stroke) {
    drawOval(
        color = color,
        topLeft = Offset(400f - rx, 400f - ry),
        size = Size(rx * 2, ry * 2),
        style = stroke,
    )
}

private fun DrawScope.drawOrbit(tint: Color, alpha: Float, angle: Float) {
    rotate(degrees = angle, pivot = Offset(400f, 400f)) {
        drawCircle(
            color = tint.copy(alpha = 0.14f * alpha),
            radius = 356f,
            center = Offset(400f, 400f),
            style = Stroke(
                width = 0.75f,
                pathEffect = PathEffect.dashPathEffect(floatArrayOf(2f, 10f)),
            ),
        )
    }
}

private fun DrawScope.drawPlate(plate: Plate, tint: Color, alpha: Float, spread: Float) {
    val bounds = plate.outline.getBounds()
    val pivot = bounds.center
    translate(left = plate.drift.dx * spread, top = plate.drift.dy * spread) {
        rotate(degrees = plate.drift.degrees * spread, pivot = pivot) {
            drawPath(plate.outline, color = tint.copy(alpha = 0.05f * alpha))
            drawPath(
                path = plate.outline,
                color = tint.copy(alpha = alpha),
                style = Stroke(width = 2f, cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
            plate.ridge?.let {
                drawPath(
                    path = it,
                    color = tint.copy(alpha = 0.4f * alpha),
                    style = Stroke(width = 1f, cap = StrokeCap.Round),
                )
            }
            plate.stipple.forEach { (x, y) ->
                drawCircle(color = tint.copy(alpha = 0.3f * alpha), radius = 1.6f, center = Offset(x, y))
            }
        }
    }
}

private fun DrawScope.drawCompassRose(tint: Color, alpha: Float) {
    val color = tint.copy(alpha = 0.5f * alpha)
    val arms = listOf(
        Offset(676f, 138f) to Offset(676f, 198f),
        Offset(646f, 168f) to Offset(706f, 168f),
        Offset(657f, 149f) to Offset(695f, 187f),
        Offset(695f, 149f) to Offset(657f, 187f),
    )
    arms.forEach { (from, to) ->
        drawLine(color, from, to, strokeWidth = 0.9f, cap = StrokeCap.Round)
    }
    drawCircle(color, radius = 6f, center = Offset(676f, 168f), style = Stroke(width = 0.9f))
    drawCircle(color, radius = 1.5f, center = Offset(676f, 168f))
}
