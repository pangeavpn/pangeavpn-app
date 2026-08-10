@file:OptIn(androidx.tv.material3.ExperimentalTvMaterial3Api::class)

package org.pangeavpn.tv.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
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
import androidx.tv.material3.Button
import androidx.tv.material3.ButtonDefaults
import androidx.tv.material3.Text as TvText
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.viewmodel.ConnectionUiState
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.core.viewmodel.SettingsViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.BrandLogo
import org.pangeavpn.ui.components.ConnectButton
import org.pangeavpn.ui.components.ServerRow
import org.pangeavpn.ui.components.StatusPill
import org.pangeavpn.ui.components.ThroughputText
import org.pangeavpn.ui.components.transportStatusLabel
import org.pangeavpn.ui.theme.BrandOrange
import org.pangeavpn.ui.theme.BrandOrangeText

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
    LaunchedEffect(Unit) {
        connectFocusRequester.requestFocus()
    }

    Row(modifier = Modifier.fillMaxSize().padding(32.dp)) {
        StatusPanel(
            status = status,
            uiState = uiState,
            connectFocusRequester = connectFocusRequester,
            onConnectClick = {
                when (status.state) {
                    ConnState.CONNECTED -> connectionViewModel.disconnect()
                    ConnState.DISCONNECTED, ConnState.ERROR -> {
                        val serverId = uiState.selectedServerId ?: uiState.servers.firstOrNull()?.id
                        serverId?.let(connectionViewModel::connect)
                    }
                    ConnState.CONNECTING, ConnState.DISCONNECTING -> Unit
                }
            },
            onSettingsClick = { showSettings = true },
            modifier = Modifier.weight(0.4f).fillMaxHeight(),
        )

        Spacer(modifier = Modifier.weight(0.02f))

        ServerPanel(
            servers = uiState.servers,
            selectedServerId = uiState.selectedServerId,
            onServerClick = { serverId ->
                connectionViewModel.selectServer(serverId)
                connectionViewModel.connect(serverId)
            },
            modifier = Modifier.weight(0.58f).fillMaxHeight(),
        )
    }

    if (showSettings) {
        SettingsOverlay(settingsViewModel = settingsViewModel, onDismiss = { showSettings = false })
    }
}

@Composable
private fun StatusPanel(
    status: TunnelStatus,
    uiState: ConnectionUiState,
    connectFocusRequester: FocusRequester,
    onConnectClick: () -> Unit,
    onSettingsClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier, horizontalAlignment = Alignment.CenterHorizontally) {
        BrandLogo()
        Spacer(modifier = Modifier.height(32.dp))

        ConnectButton(
            state = status.state,
            onClick = onConnectClick,
            modifier = Modifier.focusRequester(connectFocusRequester),
        )
        Spacer(modifier = Modifier.height(16.dp))

        Text(
            text = stringResource(stateLabelRes(status.state)),
            style = MaterialTheme.typography.titleMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(modifier = Modifier.height(4.dp))

        val serverName = uiState.servers.find { it.id == uiState.selectedServerId }?.name
        Text(
            text = serverName ?: stringResource(R.string.hero_select_server),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        if (status.state == ConnState.CONNECTED) {
            Spacer(modifier = Modifier.height(16.dp))
            ThroughputText(bytesIn = status.bytesIn, bytesOut = status.bytesOut)
        }

        Spacer(modifier = Modifier.height(24.dp))
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            val connected = status.state == ConnState.CONNECTED
            val tunEstablished = status.state == ConnState.CONNECTED ||
                status.state == ConnState.CONNECTING ||
                status.state == ConnState.DISCONNECTING
            val transportLabel = transportStatusLabel(status.activeTransport)
            if (connected && transportLabel.isNotEmpty()) {
                StatusPill(label = transportLabel, active = true)
            }
            StatusPill(label = stringResource(R.string.killswitch_title), active = tunEstablished)
        }

        Spacer(modifier = Modifier.weight(1f))

        Button(
            onClick = onSettingsClick,
            colors = ButtonDefaults.colors(containerColor = BrandOrange, contentColor = BrandOrangeText),
        ) {
            TvText("⚙ " + stringResource(R.string.settings_title))
        }
    }
}

@Composable
private fun ServerPanel(
    servers: List<Server>,
    selectedServerId: String?,
    onServerClick: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    val firstServerFocusRequester = remember { FocusRequester() }

    Column(modifier = modifier) {
        Text(
            text = stringResource(R.string.servers_title),
            style = MaterialTheme.typography.headlineSmall,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(modifier = Modifier.height(16.dp))

        if (servers.isEmpty()) {
            Text(
                text = stringResource(R.string.hero_no_servers),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            return@Column
        }

        LazyColumn(
            modifier = Modifier.fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            items(servers, key = { it.id }) { server ->
                val rowModifier = if (server.id == servers.first().id) {
                    Modifier.focusRequester(firstServerFocusRequester)
                } else {
                    Modifier
                }
                ServerRow(
                    name = server.name,
                    country = server.country,
                    load = server.load,
                    selected = server.id == selectedServerId,
                    onClick = { onServerClick(server.id) },
                    modifier = rowModifier,
                )
            }
        }
    }
}

private fun stateLabelRes(state: ConnState) = when (state) {
    ConnState.CONNECTED -> R.string.state_connected
    ConnState.CONNECTING -> R.string.state_connecting
    ConnState.DISCONNECTING -> R.string.state_disconnecting
    ConnState.ERROR -> R.string.state_error
    ConnState.DISCONNECTED -> R.string.state_disconnected
}
