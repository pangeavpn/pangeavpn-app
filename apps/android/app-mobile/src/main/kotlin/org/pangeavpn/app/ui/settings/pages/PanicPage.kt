package org.pangeavpn.app.ui.settings.pages

import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import org.pangeavpn.app.Panic
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ConfirmDialog
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.SwitchRow
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle

@Composable
fun PanicPage(onBack: () -> Unit) {
    val context = LocalContext.current
    val prefs by AppPrefs.state.collectAsState()
    var confirming by remember { mutableStateOf(false) }
    val armed = stringResource(R.string.panic_toast_on)
    val disarmed = stringResource(R.string.panic_toast_off)

    fun setEnabled(enabled: Boolean) {
        AppPrefs.update { it.copy(panicWipe = enabled) }
        Panic.syncShortcut(context)
        if (enabled) ToastCenter.show(armed, ToastStyle.SUCCESS) else ToastCenter.show(disarmed, ToastStyle.INFO)
    }

    SettingsPage(
        title = stringResource(R.string.settings_section_panic),
        onBack = onBack,
        description = stringResource(R.string.panic_description),
    ) {
        SettingsGroup {
            SwitchRow(
                title = stringResource(R.string.panic_enable_title),
                checked = prefs.panicWipe,
                onCheckedChange = { setEnabled(it) },
                hint = stringResource(R.string.panic_enable_hint),
            )
        }
        SettingsGroup {
            GhostButton(
                label = stringResource(R.string.panic_now_button),
                onClick = { confirming = true },
                destructive = true,
            )
            InfoNote(text = stringResource(R.string.panic_now_note))
        }
    }

    if (confirming) {
        ConfirmDialog(
            title = stringResource(R.string.panic_confirm_title),
            message = stringResource(R.string.panic_confirm_message),
            confirmLabel = stringResource(R.string.panic_confirm_wipe),
            onConfirm = { Panic.wipeNow(context) },
            onDismiss = { confirming = false },
            destructive = true,
            cancelLabel = stringResource(R.string.panic_confirm_cancel),
        )
    }
}
