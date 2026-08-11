package org.pangeavpn.app.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.model.HUB_METHOD_CHOICES
import org.pangeavpn.core.model.TRANSPORT_CHOICES
import org.pangeavpn.core.model.isEnabled
import org.pangeavpn.core.viewmodel.SettingsViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ChoiceRow
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.SettingsField
import org.pangeavpn.ui.components.SwitchRow
import org.pangeavpn.ui.components.hubMethodHint
import org.pangeavpn.ui.components.hubMethodTitle
import org.pangeavpn.ui.components.transportChoiceLabel
import org.pangeavpn.ui.theme.StateError

/** Connection method, censorship bypass, network tuning, the kill-switch guide
 *  and sign out, wearing the hero's cards rather than stock Material. */
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

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        modifier = modifier,
        containerColor = MaterialTheme.colorScheme.background,
    ) {
        Column(
            modifier = Modifier
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp)
                .padding(bottom = 28.dp),
        ) {
            Text(
                text = stringResource(R.string.settings_title),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 26.sp,
                fontWeight = FontWeight.Light,
                letterSpacing = (-0.6).sp,
            )

            state.error?.let { message ->
                Text(
                    text = message,
                    modifier = Modifier.padding(top = 10.dp),
                    color = StateError,
                    fontSize = 13.sp,
                    lineHeight = 18.sp,
                )
            }

            Section(
                title = stringResource(R.string.settings_transport_heading),
                description = stringResource(R.string.settings_transport_description),
            )
            Column(verticalArrangement = Arrangement.spacedBy(9.dp)) {
                TRANSPORT_CHOICES.forEach { choice ->
                    ChoiceRow(
                        label = transportChoiceLabel(choice),
                        selected = state.settings.preferredTransport == choice,
                        onClick = { settingsViewModel.setPreferredTransport(choice) },
                    )
                }
            }

            Section(
                title = stringResource(R.string.settings_censorship_heading),
                description = stringResource(R.string.settings_censorship_description),
            )
            Column(verticalArrangement = Arrangement.spacedBy(9.dp)) {
                HUB_METHOD_CHOICES.forEach { method ->
                    SwitchRow(
                        title = hubMethodTitle(method),
                        hint = hubMethodHint(method),
                        checked = state.settings.hubMethods.isEnabled(method),
                        onCheckedChange = { settingsViewModel.setHubMethod(method, it) },
                    )
                }
            }

            Section(
                title = stringResource(R.string.settings_network_heading),
                description = stringResource(R.string.settings_network_description),
            )
            SwitchRow(
                title = stringResource(R.string.settings_network_allowlan_title),
                hint = stringResource(R.string.settings_network_allowlan_hint),
                checked = state.settings.allowLan,
                onCheckedChange = settingsViewModel::setAllowLan,
            )

            CommittedField(
                label = stringResource(R.string.settings_network_dns_title),
                hint = stringResource(R.string.settings_network_dns_hint),
                placeholder = stringResource(R.string.settings_network_dns_placeholder),
                initial = state.settings.customDns.joinToString(", "),
                onCommit = settingsViewModel::setCustomDns,
            )
            CommittedField(
                label = stringResource(R.string.settings_network_mtu_title),
                hint = stringResource(R.string.settings_network_mtu_hint),
                placeholder = "",
                initial = state.settings.mtu.toString(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                transform = { it.filter(Char::isDigit) },
                onCommit = { value -> value.toIntOrNull()?.let(settingsViewModel::setMtu) },
            )

            Spacer(Modifier.height(20.dp))
            GhostButton(
                label = stringResource(R.string.split_tunnel_title),
                onClick = { showSplitTunnel = true },
            )

            Spacer(Modifier.height(26.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)

            Section(
                title = stringResource(R.string.killswitch_title),
                description = stringResource(R.string.killswitch_body),
            )
            GhostButton(
                label = stringResource(R.string.killswitch_open_settings),
                onClick = { context.startActivity(settingsViewModel.openVpnSettings()) },
            )

            Spacer(Modifier.height(26.dp))
            GhostButton(
                label = stringResource(R.string.settings_signout),
                onClick = { settingsViewModel.signOut() },
                accent = StateError,
            )

            Text(
                text = "v${BuildConfig.VERSION_NAME}",
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 18.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 11.sp,
            )
        }
    }
}

@Composable
private fun Section(title: String, description: String) {
    Spacer(Modifier.height(26.dp))
    SectionLabel(text = title)
    Text(
        text = description,
        modifier = Modifier.padding(top = 6.dp, bottom = 11.dp),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 12.sp,
        lineHeight = 17.sp,
    )
}

/** Committed on the button rather than per keystroke, so a half-typed address
 *  is never persisted. The button only appears once the value differs. */
@Composable
private fun CommittedField(
    label: String,
    hint: String,
    placeholder: String,
    initial: String,
    onCommit: (String) -> Unit,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    transform: (String) -> String = { it },
) {
    var text by remember(initial) { mutableStateOf(initial) }

    Spacer(Modifier.height(14.dp))
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        SectionLabel(text = label, modifier = Modifier.weight(1f))
        if (text != initial) {
            Text(
                text = stringResource(R.string.settings_save),
                // Padded out: the label alone is under the 24dp tap minimum.
                modifier = Modifier
                    .clickable(role = Role.Button) { onCommit(text) }
                    .padding(horizontal = 12.dp, vertical = 6.dp),
                color = MaterialTheme.colorScheme.primary,
                fontSize = 13.sp,
                fontWeight = FontWeight.SemiBold,
            )
        }
    }
    SettingsField(
        value = text,
        onValueChange = { text = transform(it) },
        placeholder = placeholder,
        modifier = Modifier.padding(top = 8.dp),
        keyboardOptions = keyboardOptions,
    )
    Text(
        text = hint,
        modifier = Modifier.padding(top = 6.dp),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 12.sp,
        lineHeight = 16.sp,
    )
}
