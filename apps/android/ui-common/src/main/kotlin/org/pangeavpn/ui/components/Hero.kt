package org.pangeavpn.ui.components

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.ui.R
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateDisconnected
import org.pangeavpn.ui.theme.StateError

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

/** Status dot plus the state name in small caps, the desktop hero's kicker line. */
@Composable
fun HeroKicker(state: ConnState, modifier: Modifier = Modifier) {
    val accent = stateAccent(state)
    val dotColor by animateColorAsState(
        targetValue = if (state == ConnState.DISCONNECTED) StateDisconnected else accent,
        animationSpec = tween(durationMillis = 400),
        label = "hero-dot",
    )

    val pulsing = state == ConnState.CONNECTING || state == ConnState.DISCONNECTING
    val pulse by rememberInfiniteTransition(label = "hero-dot-pulse").animateFloat(
        initialValue = 0.4f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 800, easing = FastOutSlowInEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "hero-dot-pulse-alpha",
    )

    Row(
        modifier = modifier,
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .alpha(if (pulsing) pulse else 1f)
                .clip(CircleShape)
                .background(dotColor),
        )
        Text(
            text = stateLabel(state).uppercase(),
            color = if (state == ConnState.DISCONNECTED) MaterialTheme.colorScheme.onSurfaceVariant else accent,
            fontSize = 11.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.8.sp,
        )
    }
}

/**
 * "The world is **apart**" — the phrase is one translated string with a slot,
 * and the word dropped into that slot carries the weight.
 */
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
        modifier = modifier,
        color = MaterialTheme.colorScheme.onSurface,
        fontSize = 34.sp,
        lineHeight = 38.sp,
        fontWeight = FontWeight.Light,
        letterSpacing = (-1).sp,
    )
}

/** Splits the "%1$s" slot out of the template so only the filler is bolded. */
internal fun emphasised(template: String, emphasis: String): AnnotatedString {
    val slot = template.indexOf("%1\$s")
    if (slot < 0) return AnnotatedString(template)
    return buildAnnotatedString {
        append(template.substring(0, slot))
        withStyle(SpanStyle(fontWeight = FontWeight.Bold)) { append(emphasis) }
        append(template.substring(slot + 4))
    }
}

@Composable
fun HeroDetail(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        modifier = modifier,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 13.sp,
        lineHeight = 18.sp,
        maxLines = 2,
        overflow = TextOverflow.Ellipsis,
    )
}

/** The SESSION / DOWN / UP / VIA strip under the headline. */
@Composable
fun HeroFacts(
    session: String,
    down: String,
    up: String,
    via: String,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(top = 14.dp),
        verticalAlignment = Alignment.Top,
    ) {
        val facts = listOf(
            stringResource(R.string.fact_session) to session,
            stringResource(R.string.fact_down) to down,
            stringResource(R.string.fact_up) to up,
            stringResource(R.string.fact_via) to via,
        )
        facts.forEachIndexed { index, (key, value) ->
            Fact(key = key, value = value, modifier = Modifier.weight(1f))
            if (index < facts.lastIndex) {
                Box(
                    modifier = Modifier
                        .padding(horizontal = 10.dp)
                        .width(1.dp)
                        .height(30.dp)
                        .background(MaterialTheme.colorScheme.outlineVariant),
                )
            }
        }
    }
}

@Composable
private fun Fact(key: String, value: String, modifier: Modifier = Modifier) {
    Column(modifier = modifier) {
        Text(
            text = key.uppercase(),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            fontSize = 9.sp,
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.3.sp,
            maxLines = 1,
        )
        Text(
            text = value,
            modifier = Modifier.padding(top = 3.dp),
            color = MaterialTheme.colorScheme.onSurface,
            fontFamily = FontFamily.Monospace,
            fontSize = 15.sp,
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
