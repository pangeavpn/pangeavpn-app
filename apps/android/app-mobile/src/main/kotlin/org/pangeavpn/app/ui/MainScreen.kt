package org.pangeavpn.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.groupRegions
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.pickNode
import org.pangeavpn.core.regionLoad
import org.pangeavpn.core.regionOfServer
import org.pangeavpn.core.regionSlots
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.AllRegionsRow
import org.pangeavpn.ui.components.ConnectButton
import org.pangeavpn.ui.components.DriftMap
import org.pangeavpn.ui.components.EM_DASH
import org.pangeavpn.ui.components.HeroDetail
import org.pangeavpn.ui.components.HeroFacts
import org.pangeavpn.ui.components.HeroHeadline
import org.pangeavpn.ui.components.HeroKicker
import org.pangeavpn.ui.components.PangeaHeader
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.ServerRow
import org.pangeavpn.ui.components.humanBytes
import org.pangeavpn.ui.components.sessionClock
import org.pangeavpn.ui.components.transportStatusLabel
import org.pangeavpn.ui.theme.LocalThemeToggle

private val ACTIVE_SUBSCRIPTION_STATUSES = setOf("active", "trialing")

/**
 * The desktop hero, stacked. Desktop puts the map beside the card because it has
 * the width; a phone does not, so the same pieces run top to bottom instead.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MainScreen(
    connectionViewModel: ConnectionViewModel,
    onConnectRequested: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val status by connectionViewModel.status.collectAsState()
    val uiState by connectionViewModel.uiState.collectAsState()

    LaunchedEffect(Unit) {
        connectionViewModel.refreshServers()
        connectionViewModel.loadSubscription()
    }

    var showServerSheet by remember { mutableStateOf(false) }
    var showSettings by remember { mutableStateOf(false) }
    val sheetState = rememberModalBottomSheetState()

    val regions = remember(uiState.servers) { groupRegions(uiState.servers) }
    val selectedRegionKey = remember(regions, uiState.selectedServerId) {
        uiState.selectedServerId?.let { regionOfServer(regions, it) }?.key
    }
    val slots = remember(regions, selectedRegionKey) { regionSlots(regions, selectedRegionKey) }

    Surface(modifier = modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(modifier = Modifier.fillMaxSize()) {
            val toggleTheme = LocalThemeToggle.current
            PangeaHeader(
                version = "v${BuildConfig.VERSION_NAME}",
                onToggleTheme = toggleTheme,
                onMenu = { showSettings = true },
            )

            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(horizontal = 20.dp)
                    .padding(bottom = 24.dp),
            ) {
                // Square, but never so tall that the connect button falls off the
                // first screen on a short phone.
                BoxWithConstraints(modifier = Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                    DriftMap(
                        state = status.state,
                        modifier = Modifier.size(minOf(maxWidth, 300.dp)),
                    )
                }

                val subscription = uiState.subscription
                if (subscription == null || subscription.status !in ACTIVE_SUBSCRIPTION_STATUSES) {
                    SubscriptionBanner(
                        expiresAt = subscription?.expiresAt,
                        modifier = Modifier.padding(bottom = 16.dp),
                    )
                }

                HeroKicker(state = status.state)
                HeroHeadline(state = status.state, modifier = Modifier.padding(top = 6.dp))
                HeroDetail(
                    text = status.detail.ifEmpty { status.serverName },
                    modifier = Modifier.padding(top = 6.dp),
                )

                val connected = status.state == ConnState.CONNECTED
                HeroFacts(
                    session = sessionClock(status.state),
                    down = if (connected) humanBytes(status.bytesIn) else EM_DASH,
                    up = if (connected) humanBytes(status.bytesOut) else EM_DASH,
                    via = transportStatusLabel(status.activeTransport).ifEmpty { EM_DASH },
                )

                SectionLabel(
                    text = stringResource(R.string.hero_region),
                    modifier = Modifier.padding(top = 20.dp, bottom = 9.dp),
                )

                if (slots.isEmpty()) {
                    Text(
                        text = stringResource(R.string.hero_no_servers),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(vertical = 12.dp),
                    )
                } else {
                    Column(verticalArrangement = Arrangement.spacedBy(9.dp)) {
                        slots.forEach { region ->
                            ServerRow(
                                name = region.name,
                                country = region.country,
                                load = regionLoad(region),
                                selected = region.key == selectedRegionKey,
                                subtitle = pickNode(region).id,
                                onClick = { connectionViewModel.selectServer(pickNode(region).id) },
                            )
                        }
                    }
                }

                AllRegionsRow(
                    moreCount = (regions.size - slots.size).coerceAtLeast(0),
                    onOpen = { showServerSheet = true },
                    onRefresh = { connectionViewModel.refreshServers() },
                    modifier = Modifier.padding(top = 9.dp),
                )

                ConnectButton(
                    state = status.state,
                    modifier = Modifier.padding(top = 18.dp),
                    onClick = {
                        when (status.state) {
                            ConnState.CONNECTED -> connectionViewModel.disconnect()
                            ConnState.DISCONNECTED, ConnState.ERROR -> {
                                onConnectRequested()
                                val selected = uiState.selectedServerId
                                if (selected == null) {
                                    showServerSheet = true
                                } else {
                                    connectionViewModel.connect(selected)
                                }
                            }
                            ConnState.CONNECTING, ConnState.DISCONNECTING -> Unit
                        }
                    },
                )
            }
        }
    }

    if (showServerSheet) {
        ModalBottomSheet(
            onDismissRequest = { showServerSheet = false },
            sheetState = sheetState,
            containerColor = MaterialTheme.colorScheme.background,
        ) {
            if (regions.isEmpty()) {
                Text(
                    text = stringResource(R.string.hero_no_servers),
                    modifier = Modifier.padding(24.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                LazyColumn(
                    modifier = Modifier.padding(horizontal = 20.dp),
                    verticalArrangement = Arrangement.spacedBy(9.dp),
                    contentPadding = PaddingValues(bottom = 24.dp),
                ) {
                    items(regions, key = { it.key }) { region ->
                        ServerRow(
                            name = region.name,
                            country = region.country,
                            load = regionLoad(region),
                            selected = region.key == selectedRegionKey,
                            subtitle = pickNode(region).id,
                            onClick = {
                                connectionViewModel.selectServer(pickNode(region).id)
                                showServerSheet = false
                            },
                        )
                    }
                }
            }
        }
    }

    if (showSettings) {
        SettingsSheet(onDismiss = { showSettings = false })
    }
}

@Composable
private fun SubscriptionBanner(expiresAt: String?, modifier: Modifier = Modifier) {
    val text = expiresAt?.let { stringResource(R.string.subscription_expires, it) }
        ?: stringResource(R.string.subscription_none)
    Box(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MaterialTheme.colorScheme.surfaceVariant)
            .padding(14.dp),
        contentAlignment = Alignment.CenterStart,
    ) {
        Text(
            text = text,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
