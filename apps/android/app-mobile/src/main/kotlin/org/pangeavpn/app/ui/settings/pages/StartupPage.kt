package org.pangeavpn.app.ui.settings.pages

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.res.stringResource
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.SwitchRow
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle

@Composable
fun StartupPage(onBack: () -> Unit) {
    val prefs by AppPrefs.state.collectAsState()
    val autoOn = stringResource(R.string.startup_toast_auto_on)
    val autoOff = stringResource(R.string.startup_toast_auto_off)
    val lockdownOn = stringResource(R.string.startup_toast_lockdown_on)
    val lockdownOff = stringResource(R.string.startup_toast_lockdown_off)

    // Lockdown already keeps Auto-connect on, so the switch underneath it cannot change anything.
    fun setAutoConnect(enabled: Boolean) {
        val current = AppPrefs.value
        if (current.lockdown || enabled == current.autoConnect) return
        AppPrefs.update { it.copy(autoConnect = enabled) }
        if (enabled) ToastCenter.show(autoOn, ToastStyle.SUCCESS) else ToastCenter.show(autoOff, ToastStyle.INFO)
    }

    fun setLockdown(enabled: Boolean) {
        if (enabled == AppPrefs.value.lockdown) return
        AppPrefs.update { it.copy(lockdown = enabled) }
        if (enabled) ToastCenter.show(lockdownOn, ToastStyle.SUCCESS) else ToastCenter.show(lockdownOff, ToastStyle.INFO)
    }

    SettingsPage(
        title = stringResource(R.string.settings_section_startup),
        onBack = onBack,
        description = stringResource(R.string.startup_description),
    ) {
        SettingsGroup {
            SwitchRow(
                title = stringResource(R.string.startup_auto_connect_title),
                checked = prefs.autoConnect || prefs.lockdown,
                onCheckedChange = { setAutoConnect(it) },
                hint = stringResource(R.string.startup_auto_connect_hint),
                enabled = !prefs.lockdown,
            )
            if (prefs.lockdown) {
                InfoNote(text = stringResource(R.string.startup_lockdown_keeps_auto), icon = Icons.Outlined.Lock)
            } else if (prefs.autoConnect) {
                InfoNote(text = stringResource(R.string.startup_android_auto_connect_note))
            }
        }
        SettingsGroup {
            SwitchRow(
                title = stringResource(R.string.startup_lockdown_title),
                checked = prefs.lockdown,
                onCheckedChange = { setLockdown(it) },
                hint = stringResource(R.string.startup_lockdown_hint),
            )
        }
    }
}
