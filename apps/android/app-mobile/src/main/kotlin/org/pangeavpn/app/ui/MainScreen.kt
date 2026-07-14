package org.pangeavpn.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.BrandLogo
import org.pangeavpn.ui.components.ConnectButton
import org.pangeavpn.ui.components.ServerRow
import org.pangeavpn.ui.components.StatusPill
import org.pangeavpn.ui.components.ThroughputText

private val ACTIVE_SUBSCRIPTION_STATUSES = setOf("active", "trialing")

/** Connect screen: hero connect button, server picker sheet, status pills, throughput,
 * subscription banner, and the settings sheet entry point. */
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

    Scaffold(
        modifier = modifier,
        topBar = {
            TopAppBar(
                title = { BrandLogo() },
                actions = {
                    IconButton(onClick = { showSettings = true }) {
                        Text("⚙", style = MaterialTheme.typography.titleLarge)
                    }
                },
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .padding(padding)
                .fillMaxSize()
                .padding(24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            val subscription = uiState.subscription
            if (subscription == null || subscription.status !in ACTIVE_SUBSCRIPTION_STATUSES) {
                SubscriptionBanner(expiresAt = subscription?.expiresAt)
                Spacer(Modifier.height(16.dp))
            }

            ConnectButton(
                state = status.state,
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

            Spacer(Modifier.height(16.dp))

            val selectedServer = uiState.servers.find { it.id == uiState.selectedServerId }
            TextButton(onClick = { showServerSheet = true }) {
                Text(selectedServer?.name ?: stringResource(R.string.hero_select_server))
            }

            Spacer(Modifier.height(16.dp))

            val connected = status.state == ConnState.CONNECTED
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                StatusPill(stringResource(R.string.pill_cloak), active = connected)
                StatusPill(stringResource(R.string.pill_wireguard), active = connected)
                StatusPill(stringResource(R.string.pill_killswitch), active = connected)
            }

            if (connected) {
                Spacer(Modifier.height(16.dp))
                ThroughputText(bytesIn = status.bytesIn, bytesOut = status.bytesOut)
            }
        }
    }

    if (showServerSheet) {
        ModalBottomSheet(
            onDismissRequest = { showServerSheet = false },
            sheetState = sheetState,
        ) {
            if (uiState.servers.isEmpty()) {
                Text(
                    text = stringResource(R.string.hero_no_servers),
                    modifier = Modifier.padding(24.dp),
                )
            } else {
                LazyColumn(modifier = Modifier.padding(horizontal = 16.dp)) {
                    items(uiState.servers, key = { it.id }) { server ->
                        ServerRow(
                            name = server.name,
                            country = server.country,
                            load = server.load,
                            selected = server.id == uiState.selectedServerId,
                            onClick = {
                                connectionViewModel.selectServer(server.id)
                                showServerSheet = false
                            },
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 4.dp),
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
    Surface(
        modifier = modifier.fillMaxWidth(),
        color = MaterialTheme.colorScheme.surfaceVariant,
        shape = MaterialTheme.shapes.medium,
    ) {
        Text(
            text = text,
            modifier = Modifier.padding(12.dp),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
