package org.pangeavpn.tv.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.pangeavpn.core.Region
import org.pangeavpn.core.groupRegions
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.pickNode
import org.pangeavpn.core.regionLoad
import org.pangeavpn.core.regionOfServer
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.core.viewmodel.SettingsViewModel
import org.pangeavpn.tv.BuildConfig
import org.pangeavpn.ui.R
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

/**
 * The desktop layout, near enough as-is: a television is landscape, so the map
 * keeps its place beside the hero rather than stacking the way the phone does.
 */
@Composable
fun MainScreen(connectionViewModel: ConnectionViewModel, settingsViewModel: SettingsViewModel) {
    LaunchedEffect(Unit) {
        connectionViewModel.refreshServers()
        connectionViewModel.loadSubscription()
    }

    val status by connectionViewModel.status.collectAsState()
    val uiState by connectionViewModel.uiState.collectAsState()
    var showSettings by remember { mutableStateOf(false) }

    val connectFocusRequester = remember { FocusRequester() }
    LaunchedEffect(Unit) { connectFocusRequester.requestFocus() }

    val regions = remember(uiState.servers) { groupRegions(uiState.servers) }
    val selectedRegionKey = remember(regions, uiState.selectedServerId) {
        uiState.selectedServerId?.let { regionOfServer(regions, it) }?.key
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            val toggleTheme = LocalThemeToggle.current
            PangeaHeader(
                version = "v${BuildConfig.VERSION_NAME}",
                onToggleTheme = toggleTheme,
                onMenu = { showSettings = true },
            )

            Row(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(horizontal = 40.dp, vertical = 16.dp),
            ) {
                BoxWithConstraints(
                    modifier = Modifier
                        .weight(0.42f)
                        .fillMaxHeight(),
                    contentAlignment = Alignment.Center,
                ) {
                    DriftMap(
                        state = status.state,
                        modifier = Modifier.size(minOf(maxWidth, maxHeight)),
                    )
                }

                Spacer(modifier = Modifier.weight(0.03f))

                HeroPanel(
                    status = status,
                    regions = regions,
                    selectedRegionKey = selectedRegionKey,
                    connectFocusRequester = connectFocusRequester,
                    onRegionClick = { region ->
                        val node = pickNode(region)
                        connectionViewModel.selectServer(node.id)
                        connectionViewModel.connect(node.id)
                    },
                    onConnectClick = {
                        when (status.state) {
                            ConnState.CONNECTED -> connectionViewModel.disconnect()
                            ConnState.DISCONNECTED, ConnState.ERROR -> {
                                val serverId = uiState.selectedServerId
                                    ?: regions.firstOrNull()?.let { pickNode(it).id }
                                serverId?.let(connectionViewModel::connect)
                            }
                            ConnState.CONNECTING, ConnState.DISCONNECTING -> Unit
                        }
                    },
                    modifier = Modifier
                        .weight(0.55f)
                        .fillMaxHeight(),
                )
            }
        }
    }

    if (showSettings) {
        SettingsOverlay(settingsViewModel = settingsViewModel, onDismiss = { showSettings = false })
    }
}

@Composable
private fun HeroPanel(
    status: TunnelStatus,
    regions: List<Region>,
    selectedRegionKey: String?,
    connectFocusRequester: FocusRequester,
    onRegionClick: (Region) -> Unit,
    onConnectClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier) {
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

        if (regions.isEmpty()) {
            Text(
                text = stringResource(R.string.hero_no_servers),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else {
            // The whole list is reachable by D-pad here, so no "all regions" door.
            LazyColumn(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f),
                verticalArrangement = Arrangement.spacedBy(9.dp),
                contentPadding = PaddingValues(bottom = 8.dp),
            ) {
                items(regions, key = { it.key }) { region ->
                    ServerRow(
                        name = region.name,
                        country = region.country,
                        load = regionLoad(region),
                        selected = region.key == selectedRegionKey,
                        subtitle = pickNode(region).id,
                        onClick = { onRegionClick(region) },
                    )
                }
            }
        }

        Spacer(modifier = Modifier.height(12.dp))

        ConnectButton(
            state = status.state,
            onClick = onConnectClick,
            modifier = Modifier.focusRequester(connectFocusRequester),
        )
    }
}
