package org.pangeavpn.ui.components

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.CallSplit
import androidx.compose.material.icons.rounded.Science
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateDisconnected
import org.pangeavpn.ui.theme.StateError
import org.pangeavpn.ui.theme.reducedMotion

/** The colour the state name, the dot and the map all agree on. */
@Composable
fun stateAccent(state: ConnState): Color = when (state) {
    ConnState.CONNECTED -> StateConnected
    ConnState.CONNECTING, ConnState.DISCONNECTING -> StateConnecting
    ConnState.ERROR -> StateError
    ConnState.DISCONNECTED -> MaterialTheme.colorScheme.onSurfaceVariant
}

@Composable
private fun stateLabel(state: ConnState): String = when (state) {
    ConnState.CONNECTED -> stringResource(R.string.state_connected)
    ConnState.CONNECTING -> stringResource(R.string.state_connecting)
    ConnState.DISCONNECTING -> stringResource(R.string.state_disconnecting)
    ConnState.ERROR -> stringResource(R.string.state_error)
    ConnState.DISCONNECTED -> stringResource(R.string.state_disconnected)
}

private data class KickerFace(val state: ConnState, val postQuantum: Boolean, val split: Boolean)

/** Status dot, the state in small caps, and the PQ and Split chips; they swap as one face. */
@Composable
fun HeroKicker(
    state: ConnState,
    modifier: Modifier = Modifier,
    postQuantum: Boolean = false,
    split: Boolean = false,
) {
    val reduced = reducedMotion()
    val pulsing = !reduced && (state == ConnState.CONNECTING || state == ConnState.DISCONNECTING)
    val dim = remember { Animatable(1f) }
    LaunchedEffect(pulsing) {
        if (pulsing) {
            dim.animateTo(0.4f, infiniteRepeatable(tween(800, easing = FastOutSlowInEasing), RepeatMode.Reverse))
        } else {
            dim.animateTo(1f, Motion.quick())
        }
    }

    Box(
        modifier = modifier
            .offset(x = (-4).dp)
            .semantics(mergeDescendants = true) {},
        contentAlignment = Alignment.CenterStart,
    ) {
        // Holds the row's height, so a chip arriving never pushes the headline down.
        PostQuantumChip(Modifier.alpha(0f).clearAndSetSemantics {})
        FadeSwap(KickerFace(state, postQuantum, split)) { face ->
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                KickerDot(face.state, opacity = { dim.value })
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(
                        text = stateLabel(face.state).uppercase(),
                        color = stateAccent(face.state),
                        fontSize = 11.sp,
                        lineHeight = 14.sp,
                        fontWeight = FontWeight.Bold,
                        letterSpacing = 1.8.sp,
                        maxLines = 1,
                    )
                    if (face.postQuantum) PostQuantumChip()
                    if (face.split) {
                        HeroChip(
                            icon = Icons.AutoMirrored.Rounded.CallSplit,
                            label = stringResource(R.string.main_split_badge),
                            spoken = stringResource(R.string.main_split_label),
                            tint = MaterialTheme.colorScheme.primary,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun KickerDot(state: ConnState, opacity: () -> Float) {
    val color = if (state == ConnState.DISCONNECTED) StateDisconnected else stateAccent(state)
    Box(
        modifier = Modifier
            .graphicsLayer { alpha = opacity() }
            .size(16.dp)
            .background(if (state == ConnState.CONNECTED) color.copy(alpha = 0.2f) else Color.Transparent, CircleShape)
            .padding(4.dp)
            .background(color, CircleShape),
    )
}

@Composable
private fun PostQuantumChip(modifier: Modifier = Modifier) {
    HeroChip(
        icon = Icons.Rounded.Science,
        label = stringResource(R.string.main_pq_badge),
        spoken = stringResource(R.string.main_pq_label),
        tint = StateConnected,
        modifier = modifier,
    )
}

@Composable
private fun HeroChip(icon: ImageVector, label: String, spoken: String, tint: Color, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier
            .clip(PillShape)
            .background(Brush.linearGradient(listOf(tint.copy(alpha = 0.16f), tint.copy(alpha = 0.04f))))
            .border(1.dp, tint.copy(alpha = 0.38f), PillShape)
            .clearAndSetSemantics { contentDescription = spoken }
            .padding(start = 6.dp, end = 8.dp, top = 3.dp, bottom = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Icon(imageVector = icon, contentDescription = null, modifier = Modifier.size(11.dp), tint = tint)
        Text(
            text = label,
            color = tint,
            fontSize = 10.sp,
            lineHeight = 13.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 0.8.sp,
            maxLines = 1,
        )
    }
}

/** "The world is **apart**": one translated string with a slot, and the word in the slot carries the weight. */
@Composable
fun HeroHeadline(state: ConnState, modifier: Modifier = Modifier) {
    val template = stringResource(
        when (state) {
            ConnState.CONNECTED -> R.string.hero_headline_connected
            ConnState.CONNECTING -> R.string.hero_headline_connecting
            ConnState.DISCONNECTING -> R.string.hero_headline_disconnecting
            ConnState.ERROR -> R.string.hero_headline_error
            ConnState.DISCONNECTED -> R.string.hero_headline_disconnected
        },
    )
    val emphasis = stringResource(
        when (state) {
            ConnState.CONNECTED -> R.string.hero_emphasis_connected
            ConnState.CONNECTING -> R.string.hero_emphasis_connecting
            ConnState.DISCONNECTING -> R.string.hero_emphasis_disconnecting
            ConnState.ERROR -> R.string.hero_emphasis_error
            ConnState.DISCONNECTED -> R.string.hero_emphasis_disconnected
        },
    )

    Text(
        text = emphasised(template, emphasis),
        modifier = modifier
            .fillMaxWidth()
            .semantics { heading() },
        color = MaterialTheme.colorScheme.onSurface,
        fontSize = 34.sp,
        lineHeight = 38.sp,
        fontWeight = FontWeight.Light,
        letterSpacing = (-1).sp,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
    )
}

internal fun emphasised(template: String, emphasis: String): AnnotatedString {
    val slot = template.indexOf("%1\$s")
    if (slot < 0) return AnnotatedString(template)
    return buildAnnotatedString {
        append(template.substring(0, slot))
        withStyle(SpanStyle(fontWeight = FontWeight.Bold)) { append(emphasis) }
        append(template.substring(slot + 4))
    }
}

/** Two lines at most; an empty line keeps its height, so the screen does not jump as states change. */
@Composable
fun HeroDetail(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text.ifEmpty { " " },
        modifier = modifier
            .fillMaxWidth()
            .then(if (text.isEmpty()) Modifier.clearAndSetSemantics {} else Modifier),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 13.sp,
        lineHeight = 18.sp,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
    )
}

// The widest each value gets, so the grid never reflows as the numbers grow mid-session.
private val WIDEST_FACTS = listOf("00:00:00", "1023.9 MB", "1023.9 MB", "VLESS+REALITY")
private const val MONO_ADVANCE = 0.6f
private val FACT_CELL_PADDING = 14.dp

private fun fourColumnWidth(valueSize: Dp): Dp {
    val characters = WIDEST_FACTS.sumOf { it.length }
    return valueSize * (characters * MONO_ADVANCE) + FACT_CELL_PADDING * (WIDEST_FACTS.size * 2) +
        (WIDEST_FACTS.size - 1).dp
}

/** SESSION / DOWN / UP / VIA: one row when the widest values fit, else two by two. */
@Composable
fun HeroFacts(
    session: String,
    down: String,
    up: String,
    via: String,
    modifier: Modifier = Modifier,
) {
    val facts = listOf(
        stringResource(R.string.fact_session) to session,
        stringResource(R.string.fact_down) to down,
        stringResource(R.string.fact_up) to up,
        stringResource(R.string.fact_via) to via,
    )
    val valueSize = with(LocalDensity.current) { 15.sp.toDp() }
    val rule = MaterialTheme.colorScheme.outlineVariant

    BoxWithConstraints(
        modifier = modifier
            .fillMaxWidth()
            .clip(PangeaCardShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(1.dp, rule, PangeaCardShape),
    ) {
        if (maxWidth >= fourColumnWidth(valueSize)) {
            FactRow(facts, rule)
        } else {
            Column {
                FactRow(facts.take(2), rule)
                Box(Modifier.fillMaxWidth().height(1.dp).background(rule))
                FactRow(facts.drop(2), rule)
            }
        }
    }
}

@Composable
private fun FactRow(cells: List<Pair<String, String>>, rule: Color) {
    Row(modifier = Modifier.fillMaxWidth()) {
        cells.forEachIndexed { index, (key, value) ->
            FactCell(key, value, leadingRule = index > 0, rule = rule, modifier = Modifier.weight(1f))
        }
    }
}

@Composable
private fun FactCell(key: String, value: String, leadingRule: Boolean, rule: Color, modifier: Modifier) {
    Column(
        modifier = modifier
            .drawBehind {
                if (leadingRule) {
                    val width = 1.dp.toPx()
                    val x = if (layoutDirection == LayoutDirection.Ltr) 0f else size.width - width
                    drawRect(rule, topLeft = Offset(x, 0f), size = Size(width, size.height))
                }
            }
            .semantics(mergeDescendants = true) {}
            .padding(horizontal = FACT_CELL_PADDING, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(3.dp),
    ) {
        Text(
            text = key.uppercase(),
            color = LocalTextSecondary.current,
            fontSize = 9.5.sp,
            lineHeight = 12.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.2.sp,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = value,
            color = MaterialTheme.colorScheme.onSurface,
            fontFamily = FontFamily.Monospace,
            fontSize = 15.sp,
            lineHeight = 20.sp,
            fontWeight = FontWeight.Medium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** Small caps section heading, e.g. REGION. */
@Composable
fun SectionLabel(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text.uppercase(),
        modifier = modifier,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 10.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = 1.6.sp,
    )
}
