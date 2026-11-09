package org.pangeavpn.app.ui.main

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.SizeTransform
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Schedule
import androidx.compose.material.icons.rounded.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.SplitTunnelStore
import org.pangeavpn.core.connection.HeroRules
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.ServersRepository
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.servers.Multihop
import org.pangeavpn.core.session.subscriptionBannerWarns
import org.pangeavpn.ui.components.EM_DASH
import org.pangeavpn.ui.components.FadeSwap
import org.pangeavpn.ui.components.HeroDetail
import org.pangeavpn.ui.components.HeroFacts
import org.pangeavpn.ui.components.HeroHeadline
import org.pangeavpn.ui.components.HeroKicker
import org.pangeavpn.ui.components.PangeaCardShape
import org.pangeavpn.ui.components.RoutePath
import org.pangeavpn.ui.components.humanBytes
import org.pangeavpn.ui.components.sessionClock
import org.pangeavpn.ui.components.transportStatusLabel
import org.pangeavpn.ui.text.heroDetailText
import org.pangeavpn.ui.text.subscriptionBannerText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.StateConnecting

/** Kicker, headline, detail line and, on a multihop route, the route strip. */
@Composable
internal fun MainHero(display: ConnState, reconnecting: Boolean, modifier: Modifier = Modifier) {
    val status by ConnectionRepository.status.collectAsState()
    val connection by ConnectionRepository.state.collectAsState()
    val servers by ServersRepository.state.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val excludedApps by SplitTunnelStore.excludedApps.collectAsState()

    val detail = heroDetailText(
        HeroRules.detail(display, status.detail, status.serverName, connection.error, connection.progress, reconnecting),
    )
    val route = if (status.entryServerId.isEmpty()) {
        null
    } else {
        Multihop.routeNode(status.entryServerId, status.entryServerName, servers.regions) to
            Multihop.routeNode(status.serverId, status.serverName, servers.regions)
    }
    val exclusions = prefs.splitTunnelRanges.isNotEmpty() || excludedApps.isNotEmpty()

    Column(modifier = modifier.fillMaxWidth()) {
        HeroKicker(
            state = display,
            postQuantum = HeroRules.showsPostQuantum(display, status.postQuantum),
            split = HeroRules.showsSplit(display, prefs.splitTunnelEnabled, exclusions),
        )
        FadeSwap(display, Modifier.padding(top = 6.dp)) { HeroHeadline(it) }
        FadeSwap(detail, Modifier.padding(top = 6.dp)) { HeroDetail(it) }
        // Keyed on presence only, so a leaving route fades out with the names it had.
        AnimatedContent(
            targetState = route,
            transitionSpec = { fadeIn(Motion.quick()) togetherWith fadeOut(Motion.quick()) using SizeTransform(clip = false) },
            contentKey = { it != null },
            label = "route",
        ) { shown ->
            if (shown != null) {
                RoutePath(
                    entry = shown.first,
                    exit = shown.second,
                    connected = display == ConnState.CONNECTED,
                    modifier = Modifier.padding(top = 10.dp),
                )
            }
        }
    }
}

/** The session follows the VPN, not the hero, so an in-place switch keeps its clock and counters. */
@Composable
internal fun MainFacts(modifier: Modifier = Modifier) {
    val status by ConnectionRepository.status.collectAsState()
    val up = status.state == ConnState.CONNECTED
    HeroFacts(
        session = sessionClock(status),
        down = if (up) humanBytes(status.bytesIn) else EM_DASH,
        up = if (up) humanBytes(status.bytesOut) else EM_DASH,
        via = transportStatusLabel(status.activeTransport).ifEmpty { EM_DASH },
        modifier = modifier,
    )
}

@Composable
internal fun SubscriptionBanner(subscription: Subscription?, modifier: Modifier = Modifier) {
    val warns = subscriptionBannerWarns(subscription)
    val tint = if (warns) StateConnecting else LocalTextSecondary.current
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(PangeaCardShape)
            .background(tint.copy(alpha = 0.08f))
            .border(1.dp, tint.copy(alpha = 0.35f), PangeaCardShape)
            .semantics(mergeDescendants = true) {}
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = if (warns) Icons.Rounded.Warning else Icons.Rounded.Schedule,
            contentDescription = null,
            modifier = Modifier.size(15.dp),
            tint = tint,
        )
        Text(
            text = subscriptionBannerText(subscription),
            modifier = Modifier.weight(1f),
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 13.sp,
            lineHeight = 18.sp,
            fontWeight = FontWeight.Medium,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
