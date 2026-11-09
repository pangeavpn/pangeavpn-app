package org.pangeavpn.tv.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.rounded.SwapHoriz
import androidx.compose.material.icons.rounded.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.disabled
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.map
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.Region
import org.pangeavpn.core.SplitTunnelStore
import org.pangeavpn.core.connection.HeroRules
import org.pangeavpn.core.connection.MainAction
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.ConnectionState
import org.pangeavpn.core.data.ServersRepository
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.servers.Multihop
import org.pangeavpn.core.session.showsSubscriptionBanner
import org.pangeavpn.core.session.subscriptionBannerWarns
import org.pangeavpn.tv.BuildConfig
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.DriftMap
import org.pangeavpn.ui.components.EM_DASH
import org.pangeavpn.ui.components.FadeSwap
import org.pangeavpn.ui.components.HeroDetail
import org.pangeavpn.ui.components.HeroFacts
import org.pangeavpn.ui.components.HeroHeadline
import org.pangeavpn.ui.components.HeroKicker
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.MainActionButton
import org.pangeavpn.ui.components.PangeaHeader
import org.pangeavpn.ui.components.RoutePath
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.components.humanBytes
import org.pangeavpn.ui.components.sessionClock
import org.pangeavpn.ui.components.transportStatusLabel
import org.pangeavpn.ui.text.heroDetailText
import org.pangeavpn.ui.text.mainActionText
import org.pangeavpn.ui.text.subscriptionBannerText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnecting

private val ActionShape = RoundedCornerShape(percent = 50)

/** iOS MainView's wide stage on a 960×540 screen: the map, and beside it the hero, the full region list and the action. */
@Composable
fun TvMainScreen(onOpenSettings: () -> Unit) {
    val status by ConnectionRepository.status.collectAsState()
    val connection by ConnectionRepository.state.collectAsState()
    val canRotate by ConnectionRepository.canRotate.collectAsState()
    val servers by ServersRepository.state.collectAsState()
    val settings by SettingsRepository.settings.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val subscription by SessionRepository.subscription.collectAsState()
    val excludedApps by SplitTunnelStore.excludedApps.collectAsState()

    val entitled = subscription?.entitled != false
    val display = HeroRules.display(status.state, connection.busy, connection.switching)
    val action = MainAction.resolve(
        state = status.state,
        busy = connection.busy,
        switching = connection.switching,
        reconnecting = status.reconnecting,
        lockdownBlocks = HeroRules.lockdownBlocks(prefs.lockdown, status.state),
        entitled = entitled,
    )
    val actionFocus = remember { FocusRequester() }
    val hidden = action == MainAction.HIDDEN

    // The remote starts on the main action, and comes back to it when an expired account is restored.
    LaunchedEffect(hidden) { if (!hidden) actionFocus.tryFocus() }

    // The detail line carries an idle failure; one that lands while the tunnel is up needs a toast.
    val currentDisplay by rememberUpdatedState(display)
    LaunchedEffect(Unit) {
        ConnectionRepository.state.map { it.error }.distinctUntilChanged().drop(1).collect { message ->
            if (!message.isNullOrEmpty() && !HeroRules.carriesError(currentDisplay)) {
                ToastCenter.show(message, ToastStyle.WARNING)
            }
        }
    }

    Column(modifier = Modifier.fillMaxSize().overscan()) {
        PangeaHeader(version = "v${BuildConfig.VERSION_NAME}", onMenu = onOpenSettings)
        Row(modifier = Modifier.fillMaxWidth().weight(1f)) {
            BoxWithConstraints(
                modifier = Modifier.weight(0.42f).fillMaxHeight(),
                contentAlignment = Alignment.Center,
            ) {
                DriftMap(
                    state = display,
                    modifier = Modifier.size(minOf(maxWidth, maxHeight)).clearAndSetSemantics {},
                )
            }
            Spacer(modifier = Modifier.width(32.dp))
            Column(modifier = Modifier.weight(0.58f).fillMaxHeight()) {
                if (showsSubscriptionBanner(subscription)) {
                    SubscriptionBanner(subscription, Modifier.padding(bottom = 12.dp))
                }
                if (entitled) {
                    Hero(
                        status = status,
                        display = display,
                        connection = connection,
                        split = HeroRules.showsSplit(
                            display,
                            prefs.splitTunnelEnabled,
                            prefs.splitTunnelRanges.isNotEmpty() || excludedApps.isNotEmpty(),
                        ),
                        regions = servers.regions,
                    )
                    TvRegionList(
                        servers = servers,
                        transport = settings.preferredTransport,
                        prefs = prefs,
                        working = connection.inFlight,
                        modifier = Modifier.weight(1f).padding(top = 14.dp),
                    )
                } else {
                    TvExpiredPanel(modifier = Modifier.weight(1f))
                }
                if (!hidden) {
                    Row(
                        modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        ActionSlot(
                            action = action,
                            onClick = {
                                when (action) {
                                    MainAction.CONNECT, MainAction.STOP, MainAction.DISCONNECT -> ConnectionRepository.toggle()
                                    MainAction.LOCKDOWN -> onOpenSettings()
                                    MainAction.PROVISIONING, MainAction.DISCONNECTING, MainAction.HIDDEN -> Unit
                                }
                            },
                            focusRequester = actionFocus,
                            modifier = Modifier.weight(1f),
                        )
                        if (entitled) {
                            IconAction(
                                icon = Icons.Rounded.SwapHoriz,
                                label = stringResource(R.string.main_rotate),
                                onClick = { if (canRotate) ConnectionRepository.rotate() },
                                modifier = Modifier.alpha(if (canRotate) 1f else 0.4f),
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun Hero(
    status: TunnelStatus,
    display: ConnState,
    connection: ConnectionState,
    split: Boolean,
    regions: List<Region>,
) {
    Column(modifier = Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        HeroKicker(
            state = display,
            postQuantum = HeroRules.showsPostQuantum(display, status.postQuantum),
            split = split,
        )
        FadeSwap(display) { HeroHeadline(state = it) }
        val detail = heroDetailText(
            HeroRules.detail(
                display = display,
                statusDetail = status.detail,
                serverName = status.serverName,
                error = connection.error,
                progress = connection.progress,
                paused = status.reconnecting,
            ),
        )
        FadeSwap(detail) { HeroDetail(text = it) }
        if (status.entryServerId.isNotEmpty()) {
            RoutePath(
                entry = Multihop.routeNode(status.entryServerId, status.entryServerName, regions),
                exit = Multihop.routeNode(status.serverId, status.serverName, regions),
                connected = display == ConnState.CONNECTED,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
        if (HeroRules.showsFacts(display)) {
            // The VPN session outlives an in-place switch, so the clock and counters follow it, not the hero.
            val live = status.state == ConnState.CONNECTED
            HeroFacts(
                session = sessionClock(status),
                down = if (live) humanBytes(status.bytesIn) else EM_DASH,
                up = if (live) humanBytes(status.bytesOut) else EM_DASH,
                via = transportStatusLabel(status.activeTransport).ifEmpty { EM_DASH },
                modifier = Modifier.padding(top = 8.dp),
            )
        }
    }
}

// One stable focus target around the action: Connect, Stop and the Lockdown note swap inside it, and the
// remote's focus stays put because the control it rests on never leaves the screen.
@Composable
private fun ActionSlot(
    action: MainAction,
    onClick: () -> Unit,
    focusRequester: FocusRequester,
    modifier: Modifier = Modifier,
) {
    var focused by remember { mutableStateOf(false) }
    val enabled = action != MainAction.PROVISIONING && action != MainAction.DISCONNECTING
    val label = mainActionText(action) ?: stringResource(R.string.main_lockdown_note)

    Box(
        modifier = modifier
            .focusRequester(focusRequester)
            .onFocusChanged { focused = it.isFocused }
            .border(
                width = 2.dp,
                color = if (focused) MaterialTheme.colorScheme.onSurface else Color.Transparent,
                shape = ActionShape,
            )
            .clickable(
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                role = Role.Button,
                onClick = onClick,
            )
            .semantics {
                contentDescription = label
                if (!enabled) disabled()
            }
            .padding(4.dp),
    ) {
        MainActionButton(
            action = action,
            onClick = onClick,
            modifier = Modifier.focusProperties { canFocus = false }.clearAndSetSemantics {},
        )
    }
}

@Composable
private fun SubscriptionBanner(subscription: Subscription?, modifier: Modifier = Modifier) {
    val warns = subscriptionBannerWarns(subscription)
    val tint = if (warns) StateConnecting else LocalTextSecondary.current
    val shape = RoundedCornerShape(12.dp)
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .background(tint.copy(alpha = 0.08f))
            .border(1.dp, tint.copy(alpha = 0.35f), shape)
            .semantics(mergeDescendants = true) {}
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = if (warns) Icons.Rounded.Warning else Icons.Outlined.Schedule,
            contentDescription = null,
            modifier = Modifier.size(16.dp),
            tint = tint,
        )
        Text(
            text = subscriptionBannerText(subscription),
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 13.sp,
            lineHeight = 18.sp,
            fontWeight = FontWeight.Medium,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
