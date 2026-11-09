package org.pangeavpn.tv.ui.settings

import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Wifi
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.settings.RangesParse
import org.pangeavpn.core.settings.SplitTunnelRanges
import org.pangeavpn.core.viewmodel.SplitTunnelViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.rangesErrorText
import org.pangeavpn.ui.theme.StateConnected

@Composable
internal fun SplitTunnelSection() {
    val prefs by AppPrefs.state.collectAsState()
    val model: SplitTunnelViewModel = viewModel()
    val apps by model.state.collectAsState()
    var editing by remember { mutableStateOf(false) }
    var refused by remember { mutableStateOf<RangesParse.Refused?>(null) }
    val rangesTitle = stringResource(R.string.split_tunnel_ranges_title)
    val toastOn = stringResource(R.string.split_tunnel_toast_on)
    val toastOff = stringResource(R.string.split_tunnel_toast_off)
    val saved = stringResource(R.string.split_tunnel_toast_saved)
    val cleared = stringResource(R.string.split_tunnel_toast_cleared)

    fun commitRanges(text: String) {
        when (val parsed = SplitTunnelRanges.parse(text)) {
            is RangesParse.Ranges -> {
                refused = null
                editing = false
                AppPrefs.update { it.copy(splitTunnelRanges = parsed.ranges) }
                if (parsed.ranges.isEmpty()) ToastCenter.show(cleared, ToastStyle.INFO) else ToastCenter.show(saved, ToastStyle.SUCCESS)
            }
            is RangesParse.Refused -> refused = parsed
        }
    }

    // Read again on each visit, so the excluded apps lead the list.
    LaunchedEffect(Unit) { model.load() }

    TvToggleRow(
        title = stringResource(R.string.split_tunnel_enabled_title),
        checked = prefs.splitTunnelEnabled,
        onToggle = { enabled ->
            if (enabled != AppPrefs.value.splitTunnelEnabled) {
                AppPrefs.update { it.copy(splitTunnelEnabled = enabled) }
                if (enabled) ToastCenter.show(toastOn, ToastStyle.SUCCESS) else ToastCenter.show(toastOff, ToastStyle.INFO)
            }
        },
        hint = stringResource(R.string.split_tunnel_enabled_hint_apps),
    )

    TvGroupLabel(rangesTitle)
    TvFieldRow(
        label = rangesTitle,
        value = SplitTunnelRanges.text(prefs.splitTunnelRanges).ifEmpty { stringResource(R.string.split_tunnel_summary_empty) },
        onClick = { editing = true },
        hint = stringResource(R.string.split_tunnel_ranges_hint),
        muted = prefs.splitTunnelRanges.isEmpty(),
    )

    TvGroupLabel(stringResource(R.string.split_tunnel_apps_title))
    if (!prefs.splitTunnelEnabled) InfoNote(text = stringResource(R.string.split_tunnel_apps_off_note))
    when {
        apps.loading -> TvParagraph(stringResource(R.string.tv_settings_split_finding))
        apps.apps.isEmpty() -> TvParagraph(stringResource(R.string.split_tunnel_apps_none))
        else -> apps.apps.forEach { app ->
            key(app.packageName) {
                TvToggleRow(
                    title = app.label,
                    checked = app.excluded,
                    onToggle = { model.toggle(app.packageName, it) },
                )
            }
        }
    }

    InfoNote(text = stringResource(R.string.split_tunnel_note_dns), icon = Icons.Outlined.Info)
    InfoNote(text = stringResource(R.string.split_tunnel_note_lan), icon = Icons.Outlined.Wifi)
    InfoNote(text = stringResource(R.string.split_tunnel_note_restart), icon = Icons.Outlined.Refresh)

    if (editing) {
        TvEditDialog(
            title = rangesTitle,
            initial = SplitTunnelRanges.text(prefs.splitTunnelRanges),
            onSave = { commitRanges(it) },
            onDismiss = {
                editing = false
                refused = null
            },
            placeholder = stringResource(R.string.split_tunnel_ranges_placeholder),
            hint = stringResource(R.string.split_tunnel_ranges_hint),
            error = refused?.let { rangesErrorText(it) },
            keyboardType = KeyboardType.Uri,
        )
    }
}

@Composable
internal fun StartupSection() {
    val prefs by AppPrefs.state.collectAsState()
    val autoOn = stringResource(R.string.startup_toast_auto_on)
    val autoOff = stringResource(R.string.startup_toast_auto_off)
    val lockdownOn = stringResource(R.string.startup_toast_lockdown_on)
    val lockdownOff = stringResource(R.string.startup_toast_lockdown_off)

    TvToggleRow(
        title = stringResource(R.string.startup_auto_connect_title),
        checked = prefs.autoConnect || prefs.lockdown,
        // Lockdown already keeps Auto-connect on, so the switch underneath it cannot change anything.
        onToggle = { enabled ->
            val current = AppPrefs.value
            if (!current.lockdown && enabled != current.autoConnect) {
                AppPrefs.update { it.copy(autoConnect = enabled) }
                if (enabled) ToastCenter.show(autoOn, ToastStyle.SUCCESS) else ToastCenter.show(autoOff, ToastStyle.INFO)
            }
        },
        hint = stringResource(R.string.startup_auto_connect_hint),
        enabled = !prefs.lockdown,
    )
    if (prefs.lockdown) {
        InfoNote(text = stringResource(R.string.startup_lockdown_keeps_auto), icon = Icons.Outlined.Lock)
    } else if (prefs.autoConnect) {
        InfoNote(text = stringResource(R.string.startup_android_auto_connect_note))
    }
    TvToggleRow(
        title = stringResource(R.string.startup_lockdown_title),
        checked = prefs.lockdown,
        onToggle = { enabled ->
            if (enabled != AppPrefs.value.lockdown) {
                AppPrefs.update { it.copy(lockdown = enabled) }
                if (enabled) ToastCenter.show(lockdownOn, ToastStyle.SUCCESS) else ToastCenter.show(lockdownOff, ToastStyle.INFO)
            }
        },
        hint = stringResource(R.string.startup_lockdown_hint),
    )
}

// Android has no per-app strict mode: the system's Always-on VPN and its block switch are the kill switch.
@Composable
internal fun KillSwitchSection() {
    val context = LocalContext.current
    val prefs by AppPrefs.state.collectAsState()
    val system by PangeaVpnService.systemVpnState.collectAsState()
    // Many TVs ship without the VPN settings screen, and starting it there would crash.
    val vpnSettings = remember(context) {
        Intent(Settings.ACTION_VPN_SETTINGS).takeIf { it.resolveActivity(context.packageManager) != null }
    }

    TvGroupLabel(stringResource(R.string.kill_switch_always_on))
    TvInfoCard {
        CheckRow(text = stringResource(R.string.kill_switch_always_held), active = true)
        CheckRow(text = stringResource(R.string.kill_switch_always_routes), active = true)
        CheckRow(text = stringResource(R.string.kill_switch_android_restart), active = prefs.autoConnect || prefs.lockdown)
    }

    TvGroupLabel(stringResource(R.string.kill_switch_android_system_title))
    TvInfoCard {
        TvParagraph(stringResource(R.string.kill_switch_android_system_body))
        val state = system
        if (state != null) {
            CheckRow(
                text = stringResource(R.string.kill_switch_android_always_on_vpn),
                active = state.alwaysOn,
                value = onOff(state.alwaysOn),
            )
            CheckRow(
                text = stringResource(R.string.kill_switch_android_block_without_vpn),
                active = state.blockWithoutVpn,
                value = onOff(state.blockWithoutVpn),
            )
        } else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            InfoNote(text = stringResource(R.string.kill_switch_android_status_unknown))
        }
    }
    if (vpnSettings != null) {
        PillButton(
            label = stringResource(R.string.killswitch_open_settings),
            onClick = { runCatching { context.startActivity(vpnSettings) } },
        )
    } else {
        InfoNote(text = stringResource(R.string.tv_settings_kill_switch_no_settings))
    }
    InfoNote(text = stringResource(R.string.kill_switch_android_lockdown_note), icon = Icons.Outlined.Lock)
}

@Composable
private fun onOff(on: Boolean): String = stringResource(if (on) R.string.toggle_on else R.string.toggle_off)

@Composable
private fun CheckRow(text: String, active: Boolean, value: String? = null) {
    Row(
        modifier = Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(
            imageVector = if (active) Icons.Rounded.CheckCircle else Icons.Outlined.RadioButtonUnchecked,
            contentDescription = null,
            modifier = Modifier.padding(top = 2.dp).size(18.dp),
            tint = if (active) StateConnected else MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = text,
            modifier = Modifier.weight(1f),
            color = if (active) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
            fontSize = 15.sp,
            lineHeight = 21.sp,
        )
        if (value != null) {
            Text(
                text = value,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontFamily = FontFamily.Monospace,
                fontSize = 14.sp,
                lineHeight = 21.sp,
            )
        }
    }
}
