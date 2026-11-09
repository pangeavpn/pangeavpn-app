package org.pangeavpn.tv.ui.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.RemoveCircleOutline
import androidx.compose.material.icons.rounded.Cancel
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateMap
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.HubMethodChange
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.model.HubTestResult
import org.pangeavpn.core.settings.DnsChoice
import org.pangeavpn.core.settings.DnsPresets
import org.pangeavpn.core.settings.HubMethodRules
import org.pangeavpn.core.settings.HubTestTone
import org.pangeavpn.core.settings.SettingsInput
import org.pangeavpn.core.settings.TransportChoices
import org.pangeavpn.core.settings.TunnelMtu
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PillBadge
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.components.hubMethodHint
import org.pangeavpn.ui.components.hubMethodTitle
import org.pangeavpn.ui.components.transportChoiceDescription
import org.pangeavpn.ui.components.transportChoiceLabel
import org.pangeavpn.ui.text.dnsChoiceTitle
import org.pangeavpn.ui.text.hubStatusText
import org.pangeavpn.ui.text.hubTestColor
import org.pangeavpn.ui.text.hubTestText
import org.pangeavpn.ui.text.mtuInvalidText
import org.pangeavpn.ui.text.settingsErrorText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateDisconnected

// A left-to-right isolate, so an address list keeps its order inside Arabic or Persian text.
private fun ltrIsolated(text: String): String = "${Char(0x2066)}$text${Char(0x2069)}"

/** Loads the core settings when a section opens, shows a failed save's note, and drops it on leaving. */
@Composable
private fun CoreSectionError() {
    LaunchedEffect(Unit) { SettingsRepository.load() }
    DisposableEffect(Unit) {
        onDispose { SettingsRepository.clearError() }
    }
    val error by SettingsRepository.error.collectAsState()
    error?.let { InfoNote(text = settingsErrorText(it), tone = NoteTone.ERROR) }
}

@Composable
private fun SectionHint(text: String) {
    Text(
        text = text,
        modifier = Modifier.padding(horizontal = 4.dp),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 13.sp,
        lineHeight = 18.sp,
    )
}

@Composable
internal fun TransportSection() {
    val settings by SettingsRepository.settings.collectAsState()
    // Shows the press at once, so the list never lags behind the remote while the core saves.
    var pending by remember { mutableStateOf<String?>(null) }
    val selected = pending ?: TransportChoices.selected(settings.preferredTransport)
    val updated = stringResource(R.string.bypass_updated)
    val failed = stringResource(R.string.network_update_failed)

    fun select(transport: String) {
        if (pending != null || transport == selected) return
        pending = transport
        settingsScope.launch {
            val saved = SettingsRepository.save { it.copy(preferredTransport = transport) }
            pending = null
            report(saved, updated, failed)
        }
    }

    CoreSectionError()
    Column(modifier = Modifier.selectableGroup(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        TransportChoices.pickerOrder.forEach { transport ->
            TvChoiceRow(
                label = transportChoiceLabel(transport),
                selected = selected == transport,
                onClick = { select(transport) },
                hint = transportChoiceDescription(transport),
                enabled = pending == null,
            )
        }
    }
    InfoNote(text = stringResource(R.string.bypass_regions_note))
}

@Composable
internal fun ProvisioningSection(results: SnapshotStateMap<String, HubTestResult>) {
    val settings by SettingsRepository.settings.collectAsState()
    val pending by SettingsRepository.pendingHubMethods.collectAsState()
    val testing by SettingsRepository.testingHubMethod.collectAsState()
    val status by SettingsRepository.hubStatus.collectAsState()
    var confirmingNormalTest by remember { mutableStateOf(false) }
    val failed = stringResource(R.string.network_update_failed)
    val deadDropOn = stringResource(R.string.provisioning_deaddrop_on)
    val deadDropOff = stringResource(R.string.provisioning_deaddrop_off)

    fun runTest(method: String) {
        if (SettingsRepository.testingHubMethod.value != null) return
        results.remove(method)
        settingsScope.launch { SettingsRepository.testHubMethod(method)?.let { results[method] = it } }
    }

    // Testing a switched-off normal connection still puts our domain on the wire, so it asks first.
    fun requestTest(method: String, on: Boolean) {
        if (SettingsRepository.testingHubMethod.value != null) return
        if (HubMethodRules.testNeedsConfirmation(method, on)) confirmingNormalTest = true else runTest(method)
    }

    LaunchedEffect(Unit) { SettingsRepository.refreshHubStatus() }
    CoreSectionError()
    ActiveMethodLine(text = hubStatusText(status), live = !status.active.isNullOrEmpty())
    HubMethodRules.order.forEach { method ->
        val on = HubMethodRules.isOn(method, settings.hubMethods, pending)
        HubMethodCard(
            method = method,
            on = on,
            locked = HubMethodRules.isLocked(method, settings.hubMethods, pending),
            inUse = status.active == method,
            result = results[method],
            testing = testing == method,
            testsLocked = testing != null,
            onTest = { requestTest(method, on) },
        )
    }
    SectionHint(stringResource(R.string.provisioning_deaddrop_description))
    TvStoreToggleRow(
        title = stringResource(R.string.settings_censorship_deaddrop_title),
        value = settings.deadDrop,
        save = { enabled ->
            val saved = SettingsRepository.save { it.copy(deadDrop = enabled) }
            report(saved, if (enabled) deadDropOn else deadDropOff, failed)
        },
        hint = stringResource(R.string.settings_censorship_deaddrop_hint),
    )

    if (confirmingNormalTest) {
        TvConfirmDialog(
            title = stringResource(R.string.provisioning_normal_test_title),
            message = stringResource(R.string.provisioning_normal_test_message),
            confirmLabel = stringResource(R.string.provisioning_normal_test_confirm),
            cancelLabel = stringResource(R.string.provisioning_normal_test_cancel),
            onDismiss = { confirmingNormalTest = false },
            onConfirm = { runTest("normal") },
        )
    }
}

@Composable
private fun ActiveMethodLine(text: String, live: Boolean) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(horizontal = 4.dp).semantics(mergeDescendants = true) {},
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .background(if (live) StateConnected else StateDisconnected, CircleShape),
        )
        Text(
            text = text,
            color = LocalTextSecondary.current,
            fontSize = 14.sp,
            fontWeight = FontWeight.Medium,
        )
    }
}

@Composable
private fun HubMethodCard(
    method: String,
    on: Boolean,
    locked: Boolean,
    inUse: Boolean,
    result: HubTestResult?,
    testing: Boolean,
    testsLocked: Boolean,
    onTest: () -> Unit,
) {
    val title = hubMethodTitle(method)
    val onText = stringResource(R.string.provisioning_method_on, title)
    val offText = stringResource(R.string.provisioning_method_off, title)
    val failed = stringResource(R.string.network_update_failed)
    val inUseText = stringResource(R.string.provisioning_in_use)

    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        TvToggleRow(
            title = title,
            checked = on,
            onToggle = { enabled ->
                settingsScope.launch {
                    when (SettingsRepository.setHubMethod(method, enabled)) {
                        HubMethodChange.SAVED -> ToastCenter.show(if (enabled) onText else offText, ToastStyle.SUCCESS)
                        HubMethodChange.IGNORED -> Unit
                        HubMethodChange.LAST_METHOD, HubMethodChange.FAILED -> ToastCenter.show(failed, ToastStyle.WARNING)
                    }
                }
            },
            hint = hubMethodHint(method),
            note = if (locked) stringResource(R.string.settings_censorship_last_method) else null,
            enabled = !locked,
            badge = if (inUse) {
                { PillBadge(text = inUseText, color = StateConnected) }
            } else {
                null
            },
        )
        Row(
            modifier = Modifier.padding(start = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            // The running test's button keeps its focus; the others wait their turn.
            GhostButton(
                label = stringResource(if (testing) R.string.provisioning_testing else R.string.provisioning_test),
                onClick = onTest,
                enabled = testing || !testsLocked,
                compact = true,
                accent = MaterialTheme.colorScheme.primary,
            )
            if (result != null && !testing) TestResultLine(result, Modifier.weight(1f))
        }
    }
}

@Composable
private fun TestResultLine(result: HubTestResult, modifier: Modifier = Modifier) {
    val tone = HubMethodRules.outcome(result).tone
    Row(
        modifier = modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(
            imageVector = when (tone) {
                HubTestTone.OK -> Icons.Rounded.CheckCircle
                HubTestTone.UNAVAILABLE -> Icons.Outlined.RemoveCircleOutline
                HubTestTone.FAILED -> Icons.Rounded.Cancel
            },
            contentDescription = null,
            modifier = Modifier.size(16.dp),
            tint = hubTestColor(result),
        )
        Text(
            text = hubTestText(result),
            color = LocalTextSecondary.current,
            fontSize = 13.sp,
            lineHeight = 18.sp,
        )
    }
}

private enum class NetworkField { DNS, MTU }

@Composable
internal fun NetworkSection() {
    val settings by SettingsRepository.settings.collectAsState()
    val resources = LocalContext.current.resources
    // Custom stays picked while its field is empty or matches a preset, as on desktop.
    var customChosen by remember { mutableStateOf(false) }
    var dnsSaving by remember { mutableStateOf(false) }
    var dnsInvalid by remember { mutableStateOf(false) }
    var mtuInvalid by remember { mutableStateOf(false) }
    var editing by remember { mutableStateOf<NetworkField?>(null) }
    val dnsChoice = if (customChosen) DnsChoice.CUSTOM else DnsPresets.choice(settings.customDns)
    val failed = stringResource(R.string.network_update_failed)
    val customDnsTitle = stringResource(R.string.settings_network_dns_title)
    val mtuTitle = stringResource(R.string.settings_network_mtu_title)

    fun saveDns(servers: List<String>) {
        dnsSaving = true
        settingsScope.launch {
            val saved = SettingsRepository.save { it.copy(customDns = servers) }
            dnsSaving = false
            if (saved) customChosen = false
            val stored = SettingsRepository.settings.value.customDns
            val success = if (stored.isEmpty()) {
                resources.getString(R.string.network_dns_default_saved)
            } else {
                resources.getString(R.string.network_dns_saved, ltrIsolated(stored.joinToString(", ")))
            }
            report(saved, success, failed)
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
        editing = null
        saveDns(servers)
    }

    fun commitMtu(text: String) {
        val mtu = TunnelMtu.parse(text)
        if (mtu == null) {
            mtuInvalid = true
            return
        }
        mtuInvalid = false
        editing = null
        settingsScope.launch {
            val saved = SettingsRepository.save { it.copy(mtu = mtu) }
            report(saved, resources.getString(R.string.network_mtu_saved, SettingsRepository.settings.value.mtu), failed)
        }
    }

    CoreSectionError()
    TvStoreToggleRow(
        title = stringResource(R.string.settings_network_allowlan_title),
        value = settings.allowLan,
        save = { enabled ->
            val saved = SettingsRepository.save { it.copy(allowLan = enabled) }
            report(
                saved,
                resources.getString(if (enabled) R.string.network_allowlan_on else R.string.network_allowlan_off),
                failed,
            )
        },
        hint = stringResource(R.string.settings_network_allowlan_hint),
    )
    TvStoreToggleRow(
        title = stringResource(R.string.network_pq_title),
        value = settings.postQuantum,
        save = { enabled ->
            val saved = SettingsRepository.save { it.copy(postQuantum = enabled) }
            report(saved, resources.getString(if (enabled) R.string.network_pq_on else R.string.network_pq_off), failed)
        },
        hint = stringResource(R.string.network_pq_hint),
    )

    TvGroupLabel(stringResource(R.string.network_dns_provider_title))
    Column(modifier = Modifier.selectableGroup(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        DnsChoice.entries.forEach { choice ->
            TvChoiceRow(
                label = dnsChoiceTitle(choice),
                selected = dnsChoice == choice,
                onClick = { choose(choice) },
                enabled = !dnsSaving,
            )
        }
    }
    SectionHint(stringResource(R.string.network_dns_provider_hint))
    if (dnsChoice == DnsChoice.CUSTOM) {
        TvFieldRow(
            label = customDnsTitle,
            value = settings.customDns.joinToString(", ").ifEmpty { stringResource(R.string.network_dns_automatic) },
            onClick = { editing = NetworkField.DNS },
            hint = stringResource(R.string.settings_network_dns_hint),
            muted = settings.customDns.isEmpty(),
        )
    }

    TvGroupLabel(mtuTitle)
    TvFieldRow(
        label = mtuTitle,
        value = settings.mtu.toString(),
        onClick = { editing = NetworkField.MTU },
        hint = stringResource(R.string.settings_network_mtu_hint),
    )

    when (editing) {
        NetworkField.DNS -> TvEditDialog(
            title = customDnsTitle,
            initial = settings.customDns.joinToString(", "),
            onSave = { commitCustomDns(it) },
            onDismiss = {
                editing = null
                dnsInvalid = false
            },
            placeholder = stringResource(R.string.settings_network_dns_placeholder),
            hint = stringResource(R.string.settings_network_dns_hint),
            error = if (dnsInvalid) stringResource(R.string.settings_network_dns_invalid) else null,
            keyboardType = KeyboardType.Uri,
            transform = DnsPresets::typed,
        )
        NetworkField.MTU -> TvEditDialog(
            title = mtuTitle,
            initial = settings.mtu.toString(),
            onSave = { commitMtu(it) },
            onDismiss = {
                editing = null
                mtuInvalid = false
            },
            placeholder = TunnelMtu.DEFAULT.toString(),
            hint = stringResource(R.string.settings_network_mtu_hint),
            error = if (mtuInvalid) mtuInvalidText() else null,
            keyboardType = KeyboardType.Number,
            transform = SettingsInput::asciiDigits,
        )
        null -> Unit
    }
}
