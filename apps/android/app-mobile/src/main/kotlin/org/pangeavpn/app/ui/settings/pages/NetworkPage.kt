package org.pangeavpn.app.ui.settings.pages

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Check
import androidx.compose.material.icons.rounded.UnfoldMore
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.settings.DnsChoice
import org.pangeavpn.core.settings.DnsPresets
import org.pangeavpn.core.settings.SettingsInput
import org.pangeavpn.core.settings.TunnelMtu
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.CommittedField
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.StoreSwitchRow
import org.pangeavpn.ui.text.dnsChoiceTitle
import org.pangeavpn.ui.text.mtuInvalidText
import org.pangeavpn.ui.theme.Motion

@Composable
fun NetworkPage(onBack: () -> Unit) {
    val error = networkPageError()
    val settings by SettingsRepository.settings.collectAsState()
    val resources = LocalContext.current.resources
    val scope = rememberCoroutineScope()
    // Custom stays picked while its field is empty or matches a preset, as on desktop.
    var customChosen by remember { mutableStateOf(false) }
    var dnsSaving by remember { mutableStateOf(false) }
    var dnsInvalid by remember { mutableStateOf(false) }
    var mtuInvalid by remember { mutableStateOf(false) }
    // Bumped after a save, so a field showing an equivalent spelling resets to the stored value.
    var dnsRevision by remember { mutableIntStateOf(0) }
    var mtuRevision by remember { mutableIntStateOf(0) }
    val dnsChoice = if (customChosen) DnsChoice.CUSTOM else DnsPresets.choice(settings.customDns)
    val providerTitle = stringResource(R.string.network_dns_provider_title)
    val customDnsTitle = stringResource(R.string.settings_network_dns_title)
    val mtuTitle = stringResource(R.string.settings_network_mtu_title)

    fun saveDns(servers: List<String>) {
        dnsSaving = true
        scope.launch {
            val saved = SettingsRepository.save { it.copy(customDns = servers) }
            dnsSaving = false
            if (saved) {
                customChosen = false
                dnsRevision++
            }
            val stored = SettingsRepository.settings.value.customDns
            val success = if (stored.isEmpty()) {
                resources.getString(R.string.network_dns_default_saved)
            } else {
                resources.getString(R.string.network_dns_saved, ltrIsolated(stored.joinToString(", ")))
            }
            reportNetworkSave(resources, saved, success)
        }
    }

    fun choose(choice: DnsChoice) {
        if (dnsSaving) return
        dnsInvalid = false
        val servers = DnsPresets.servers(choice)
        if (servers == null) {
            customChosen = true
            return
        }
        customChosen = false
        if (servers != settings.customDns) saveDns(servers)
    }

    fun commitCustomDns(text: String) {
        val servers = DnsPresets.normalize(text)
        if (servers == null) {
            dnsInvalid = true
            return
        }
        dnsInvalid = false
        saveDns(servers)
    }

    fun commitMtu(text: String) {
        val mtu = TunnelMtu.parse(text)
        if (mtu == null) {
            mtuInvalid = true
            return
        }
        mtuInvalid = false
        scope.launch {
            val saved = SettingsRepository.save { it.copy(mtu = mtu) }
            if (saved) mtuRevision++
            reportNetworkSave(
                resources,
                saved,
                resources.getString(R.string.network_mtu_saved, SettingsRepository.settings.value.mtu),
            )
        }
    }

    SettingsPage(
        title = stringResource(R.string.settings_network_heading),
        onBack = onBack,
        description = stringResource(R.string.settings_network_description),
        error = error,
    ) {
        SettingsGroup {
            StoreSwitchRow(
                title = stringResource(R.string.settings_network_allowlan_title),
                value = settings.allowLan,
                save = { enabled ->
                    val saved = SettingsRepository.save { it.copy(allowLan = enabled) }
                    reportNetworkSave(
                        resources,
                        saved,
                        resources.getString(if (enabled) R.string.network_allowlan_on else R.string.network_allowlan_off),
                    )
                },
                hint = stringResource(R.string.settings_network_allowlan_hint),
            )
            StoreSwitchRow(
                title = stringResource(R.string.network_pq_title),
                value = settings.postQuantum,
                save = { enabled ->
                    val saved = SettingsRepository.save { it.copy(postQuantum = enabled) }
                    reportNetworkSave(
                        resources,
                        saved,
                        resources.getString(if (enabled) R.string.network_pq_on else R.string.network_pq_off),
                    )
                },
                hint = stringResource(R.string.network_pq_hint),
            )
        }
        SettingsGroup(title = providerTitle) {
            DnsProviderMenu(selection = dnsChoice, enabled = !dnsSaving, onChoose = { choose(it) })
            NetworkHint(stringResource(R.string.network_dns_provider_hint))
        }
        AnimatedVisibility(
            visible = dnsChoice == DnsChoice.CUSTOM,
            enter = fadeIn(Motion.quick()) + expandVertically(Motion.quick()),
            exit = fadeOut(Motion.quick()) + shrinkVertically(Motion.quick()),
        ) {
            SettingsGroup(title = customDnsTitle) {
                key(dnsRevision) {
                    // Addresses read left to right in every language, so the field does too.
                    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
                        CommittedField(
                            label = customDnsTitle,
                            initial = settings.customDns.joinToString(", "),
                            onCommit = { commitCustomDns(it) },
                            placeholder = stringResource(R.string.settings_network_dns_placeholder),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                            transform = DnsPresets::typed,
                        )
                    }
                }
                NetworkHint(stringResource(R.string.settings_network_dns_hint))
                if (dnsInvalid) {
                    InfoNote(text = stringResource(R.string.settings_network_dns_invalid), tone = NoteTone.ERROR)
                }
            }
        }
        SettingsGroup(title = mtuTitle) {
            key(mtuRevision) {
                CommittedField(
                    label = mtuTitle,
                    initial = settings.mtu.toString(),
                    onCommit = { commitMtu(it) },
                    placeholder = TunnelMtu.DEFAULT.toString(),
                    hint = stringResource(R.string.settings_network_mtu_hint),
                    error = if (mtuInvalid) mtuInvalidText() else null,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    transform = SettingsInput::asciiDigits,
                )
            }
        }
    }
}

@Composable
private fun DnsProviderMenu(selection: DnsChoice, enabled: Boolean, onChoose: (DnsChoice) -> Unit) {
    var expanded by remember { mutableStateOf(false) }

    Box {
        PangeaCard(
            padding = PaddingValues(horizontal = 14.dp, vertical = 13.dp),
            onClick = { if (enabled) expanded = true },
            modifier = Modifier
                .heightIn(min = 48.dp)
                .alpha(if (enabled) 1f else 0.6f),
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Text(
                    text = dnsChoiceTitle(selection),
                    modifier = Modifier.weight(1f),
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 15.sp,
                )
                Icon(
                    imageVector = Icons.Rounded.UnfoldMore,
                    contentDescription = null,
                    modifier = Modifier.size(18.dp),
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            DnsChoice.entries.forEach { choice ->
                DropdownMenuItem(
                    text = { Text(dnsChoiceTitle(choice)) },
                    onClick = {
                        expanded = false
                        onChoose(choice)
                    },
                    modifier = Modifier.semantics { selected = choice == selection },
                    trailingIcon = {
                        if (choice == selection) {
                            Icon(
                                imageVector = Icons.Rounded.Check,
                                contentDescription = null,
                                tint = MaterialTheme.colorScheme.primary,
                            )
                        }
                    },
                )
            }
        }
    }
}
