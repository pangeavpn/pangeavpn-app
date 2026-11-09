package org.pangeavpn.app.ui.settings.pages

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.RemoveCircleOutline
import androidx.compose.material.icons.rounded.Cancel
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.HubMethodChange
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.model.HubStatus
import org.pangeavpn.core.model.HubTestResult
import org.pangeavpn.core.settings.HubMethodRules
import org.pangeavpn.core.settings.HubTestTone
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ConfirmDialog
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.PillBadge
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.StoreSwitchRow
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.components.hubMethodHint
import org.pangeavpn.ui.components.hubMethodTitle
import org.pangeavpn.ui.text.hubStatusText
import org.pangeavpn.ui.text.hubTestColor
import org.pangeavpn.ui.text.hubTestText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateDisconnected

@Composable
fun ProvisioningPage(onBack: () -> Unit) {
    val error = networkPageError()
    val settings by SettingsRepository.settings.collectAsState()
    val pending by SettingsRepository.pendingHubMethods.collectAsState()
    val testing by SettingsRepository.testingHubMethod.collectAsState()
    val status by SettingsRepository.hubStatus.collectAsState()
    val results = remember { mutableStateMapOf<String, HubTestResult>() }
    var confirmingNormalTest by remember { mutableStateOf(false) }
    val resources = LocalContext.current.resources
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) { SettingsRepository.refreshHubStatus() }

    // Undispatched, so a tap counts as pending before the next frame reads the switches.
    fun setMethod(method: String, title: String, enabled: Boolean) {
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            when (SettingsRepository.setHubMethod(method, enabled)) {
                HubMethodChange.SAVED -> ToastCenter.show(
                    resources.getString(
                        if (enabled) R.string.provisioning_method_on else R.string.provisioning_method_off,
                        title,
                    ),
                    ToastStyle.SUCCESS,
                )
                HubMethodChange.IGNORED -> Unit
                HubMethodChange.LAST_METHOD, HubMethodChange.FAILED ->
                    ToastCenter.show(resources.getString(R.string.network_update_failed), ToastStyle.WARNING)
            }
        }
    }

    fun runTest(method: String) {
        if (SettingsRepository.testingHubMethod.value != null) return
        results.remove(method)
        scope.launch(start = CoroutineStart.UNDISPATCHED) {
            SettingsRepository.testHubMethod(method)?.let { results[method] = it }
        }
    }

    fun requestTest(method: String, on: Boolean) {
        if (SettingsRepository.testingHubMethod.value != null) return
        if (HubMethodRules.testNeedsConfirmation(method, on)) confirmingNormalTest = true else runTest(method)
    }

    SettingsPage(
        title = stringResource(R.string.settings_censorship_heading),
        onBack = onBack,
        description = stringResource(R.string.settings_censorship_description),
        error = error,
    ) {
        ActiveMethodLine(status)
        SettingsGroup {
            HubMethodRules.order.forEach { method ->
                val title = hubMethodTitle(method)
                val on = HubMethodRules.isOn(method, settings.hubMethods, pending)
                HubMethodRow(
                    title = title,
                    hint = hubMethodHint(method),
                    on = on,
                    locked = HubMethodRules.isLocked(method, settings.hubMethods, pending),
                    inUse = status.active == method,
                    result = results[method],
                    testing = testing == method,
                    testsLocked = testing != null,
                    onToggle = { setMethod(method, title, it) },
                    onTest = { requestTest(method, on) },
                )
            }
        }
        SettingsGroup {
            NetworkHint(stringResource(R.string.provisioning_deaddrop_description))
            StoreSwitchRow(
                title = stringResource(R.string.settings_censorship_deaddrop_title),
                value = settings.deadDrop,
                save = { enabled ->
                    val saved = SettingsRepository.save { it.copy(deadDrop = enabled) }
                    reportNetworkSave(
                        resources,
                        saved,
                        resources.getString(
                            if (enabled) R.string.provisioning_deaddrop_on else R.string.provisioning_deaddrop_off,
                        ),
                    )
                },
                hint = stringResource(R.string.settings_censorship_deaddrop_hint),
            )
        }
    }

    // Testing a switched-off normal connection still puts our domain on the wire, so it asks first.
    if (confirmingNormalTest) {
        ConfirmDialog(
            title = stringResource(R.string.provisioning_normal_test_title),
            message = stringResource(R.string.provisioning_normal_test_message),
            confirmLabel = stringResource(R.string.provisioning_normal_test_confirm),
            onConfirm = { runTest("normal") },
            onDismiss = { confirmingNormalTest = false },
            cancelLabel = stringResource(R.string.provisioning_normal_test_cancel),
        )
    }
}

@Composable
private fun ActiveMethodLine(status: HubStatus) {
    val live = !status.active.isNullOrEmpty()
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 4.dp)
            .semantics(mergeDescendants = true) {},
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(
            modifier = Modifier
                .size(8.dp)
                .background(if (live) StateConnected else StateDisconnected, CircleShape),
        )
        Text(
            text = hubStatusText(status),
            color = LocalTextSecondary.current,
            fontSize = 13.sp,
            lineHeight = 18.sp,
            fontWeight = FontWeight.Medium,
        )
    }
}

@Composable
private fun HubMethodRow(
    title: String,
    hint: String,
    on: Boolean,
    locked: Boolean,
    inUse: Boolean,
    result: HubTestResult?,
    testing: Boolean,
    testsLocked: Boolean,
    onToggle: (Boolean) -> Unit,
    onTest: () -> Unit,
) {
    val testingLabel = stringResource(R.string.provisioning_testing)
    val testLabel = stringResource(R.string.provisioning_test_method, title)

    PangeaCard(padding = PaddingValues(0.dp)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .toggleable(value = on, enabled = !locked, role = Role.Switch, onValueChange = onToggle)
                .heightIn(min = 48.dp)
                .padding(start = 14.dp, end = 10.dp, top = 10.dp, bottom = 4.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(
                        text = title,
                        modifier = Modifier.weight(1f, fill = false),
                        color = MaterialTheme.colorScheme.onSurface,
                        fontSize = 15.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                    if (inUse) PillBadge(text = stringResource(R.string.provisioning_in_use), color = StateConnected)
                }
                Text(
                    text = hint,
                    modifier = Modifier.padding(top = 2.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontSize = 12.sp,
                    lineHeight = 16.sp,
                )
                if (locked) {
                    Text(
                        text = stringResource(R.string.settings_censorship_last_method),
                        modifier = Modifier.padding(top = 2.dp),
                        color = LocalTextSecondary.current,
                        fontSize = 12.sp,
                        lineHeight = 16.sp,
                        fontWeight = FontWeight.Medium,
                    )
                }
            }
            Switch(checked = on, onCheckedChange = null, enabled = !locked, colors = networkSwitchColors())
        }
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(start = 14.dp, end = 14.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            GhostButton(
                label = if (testing) testingLabel else stringResource(R.string.provisioning_test),
                onClick = onTest,
                enabled = testing || !testsLocked,
                compact = true,
                accent = MaterialTheme.colorScheme.primary,
                modifier = Modifier.semantics { contentDescription = if (testing) testingLabel else testLabel },
            )
            // Always present, so TalkBack reads the result out when it arrives.
            Box(
                modifier = Modifier
                    .weight(1f)
                    .semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
            ) {
                if (result != null && !testing) ResultLine(result)
            }
        }
    }
}

@Composable
private fun ResultLine(result: HubTestResult) {
    val icon = when (HubMethodRules.outcome(result).tone) {
        HubTestTone.OK -> Icons.Rounded.CheckCircle
        HubTestTone.UNAVAILABLE -> Icons.Outlined.RemoveCircleOutline
        HubTestTone.FAILED -> Icons.Rounded.Cancel
    }
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.padding(top = 1.dp).size(14.dp),
            tint = hubTestColor(result),
        )
        Text(
            text = hubTestText(result),
            color = LocalTextSecondary.current,
            fontSize = 12.sp,
            lineHeight = 16.sp,
        )
    }
}
