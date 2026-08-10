package org.pangeavpn.app.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.model.HUB_METHOD_CHOICES
import org.pangeavpn.core.model.TRANSPORT_CHOICES
import org.pangeavpn.core.model.isEnabled
import org.pangeavpn.core.viewmodel.SettingsViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.hubMethodHint
import org.pangeavpn.ui.components.hubMethodTitle
import org.pangeavpn.ui.components.transportChoiceLabel

/** Connection method, censorship bypass, network tuning, the kill-switch guide
 *  and sign out. Mirrors the desktop settings panel. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsSheet(
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    settingsViewModel: SettingsViewModel = viewModel(),
) {
    val context = LocalContext.current
    val state by settingsViewModel.state.collectAsState()
    var showSplitTunnel by remember { mutableStateOf(false) }

    if (showSplitTunnel) {
        SplitTunnelSheet(onDismiss = { showSplitTunnel = false })
    }

    ModalBottomSheet(onDismissRequest = onDismiss, modifier = modifier) {
        Column(
            modifier = Modifier
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp, vertical = 16.dp),
        ) {
            Text(stringResource(R.string.settings_title), style = MaterialTheme.typography.headlineSmall)

            state.error?.let { message ->
                Spacer(Modifier.height(8.dp))
                Text(
                    text = message,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }

            SectionHeading(
                title = stringResource(R.string.settings_transport_heading),
                description = stringResource(R.string.settings_transport_description),
            )
            TRANSPORT_CHOICES.forEach { choice ->
                ChoiceRow(
                    label = transportChoiceLabel(choice),
                    selected = state.settings.preferredTransport == choice,
                    onSelect = { settingsViewModel.setPreferredTransport(choice) },
                )
            }

            SectionHeading(
                title = stringResource(R.string.settings_censorship_heading),
                description = stringResource(R.string.settings_censorship_description),
            )
            HUB_METHOD_CHOICES.forEach { method ->
                ToggleRow(
                    title = hubMethodTitle(method),
                    hint = hubMethodHint(method),
                    checked = state.settings.hubMethods.isEnabled(method),
                    onCheckedChange = { settingsViewModel.setHubMethod(method, it) },
                )
            }

            SectionHeading(
                title = stringResource(R.string.settings_network_heading),
                description = stringResource(R.string.settings_network_description),
            )
            ToggleRow(
                title = stringResource(R.string.settings_network_allowlan_title),
                hint = stringResource(R.string.settings_network_allowlan_hint),
                checked = state.settings.allowLan,
                onCheckedChange = settingsViewModel::setAllowLan,
            )
            DnsField(
                initial = state.settings.customDns.joinToString(", "),
                onCommit = settingsViewModel::setCustomDns,
            )
            MtuField(initial = state.settings.mtu, onCommit = settingsViewModel::setMtu)

            Spacer(Modifier.height(16.dp))
            OutlinedButton(onClick = { showSplitTunnel = true }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.split_tunnel_title))
            }

            Spacer(Modifier.height(24.dp))
            HorizontalDivider()
            Spacer(Modifier.height(16.dp))

            Text(stringResource(R.string.killswitch_title), style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.killswitch_body),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
            OutlinedButton(
                onClick = { context.startActivity(settingsViewModel.openVpnSettings()) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(stringResource(R.string.killswitch_open_settings))
            }

            Spacer(Modifier.height(24.dp))
            TextButton(onClick = { settingsViewModel.signOut() }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.settings_signout))
            }

            Spacer(Modifier.height(16.dp))
            Text(
                text = "v${BuildConfig.VERSION_NAME}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(24.dp))
        }
    }
}

@Composable
private fun SectionHeading(title: String, description: String) {
    Spacer(Modifier.height(24.dp))
    Text(title, style = MaterialTheme.typography.titleMedium)
    Spacer(Modifier.height(2.dp))
    Text(
        text = description,
        style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
    Spacer(Modifier.height(8.dp))
}

@Composable
private fun ChoiceRow(label: String, selected: Boolean, onSelect: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .selectable(selected = selected, onClick = onSelect)
            .padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = null)
        Text(
            text = label,
            modifier = Modifier.padding(start = 8.dp),
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

@Composable
private fun ToggleRow(title: String, hint: String, checked: Boolean, onCheckedChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(
            modifier = Modifier
                .weight(1f)
                .padding(end = 12.dp),
        ) {
            Text(title, style = MaterialTheme.typography.bodyMedium)
            Text(
                text = hint,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = checked, onCheckedChange = onCheckedChange)
    }
}

/** Committed on the button rather than per keystroke, so a half-typed address
 *  is never persisted. */
@Composable
private fun DnsField(initial: String, onCommit: (String) -> Unit) {
    var text by remember(initial) { mutableStateOf(initial) }
    Spacer(Modifier.height(8.dp))
    OutlinedTextField(
        value = text,
        onValueChange = { text = it },
        label = { Text(stringResource(R.string.settings_network_dns_title)) },
        placeholder = { Text(stringResource(R.string.settings_network_dns_placeholder)) },
        supportingText = { Text(stringResource(R.string.settings_network_dns_hint)) },
        singleLine = true,
        modifier = Modifier.fillMaxWidth(),
    )
    TextButton(onClick = { onCommit(text) }) {
        Text(stringResource(R.string.settings_save))
    }
}

@Composable
private fun MtuField(initial: Int, onCommit: (Int) -> Unit) {
    var text by remember(initial) { mutableStateOf(initial.toString()) }
    Spacer(Modifier.height(8.dp))
    OutlinedTextField(
        value = text,
        onValueChange = { value -> text = value.filter(Char::isDigit) },
        label = { Text(stringResource(R.string.settings_network_mtu_title)) },
        supportingText = { Text(stringResource(R.string.settings_network_mtu_hint)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
        modifier = Modifier.fillMaxWidth(),
    )
    TextButton(onClick = { text.toIntOrNull()?.let(onCommit) }) {
        Text(stringResource(R.string.settings_save))
    }
}
