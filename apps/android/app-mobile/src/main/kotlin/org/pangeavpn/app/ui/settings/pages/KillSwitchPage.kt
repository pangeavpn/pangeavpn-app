package org.pangeavpn.app.ui.settings.pages

import android.content.Intent
import android.os.Build
import android.provider.Settings
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PowerSettingsNew
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.settings.SettingsSummary
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsNavRow
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.text.summaryText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnected

// Android has no per-app strict mode: the system's Always-on VPN and its block switch are the kill switch.
@Composable
fun KillSwitchPage(onBack: () -> Unit, onOpenStartup: () -> Unit) {
    val context = LocalContext.current
    val prefs by AppPrefs.state.collectAsState()
    val system by PangeaVpnService.systemVpnState.collectAsState()
    val vpnSettings = remember(context) {
        Intent(Settings.ACTION_VPN_SETTINGS).takeIf { it.resolveActivity(context.packageManager) != null }
    }

    SettingsPage(
        title = stringResource(R.string.settings_section_kill_switch),
        onBack = onBack,
        description = stringResource(R.string.kill_switch_description),
    ) {
        SettingsGroup(title = stringResource(R.string.kill_switch_always_on)) {
            PangeaCard {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    CheckRow(text = stringResource(R.string.kill_switch_always_held), active = true)
                    CheckRow(text = stringResource(R.string.kill_switch_always_routes), active = true)
                    CheckRow(
                        text = stringResource(R.string.kill_switch_android_restart),
                        active = prefs.autoConnect || prefs.lockdown,
                    )
                }
            }
        }

        SettingsGroup(title = stringResource(R.string.kill_switch_android_system_title)) {
            PangeaCard {
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(
                        text = stringResource(R.string.kill_switch_android_system_body),
                        color = LocalTextSecondary.current,
                        fontSize = 14.sp,
                        lineHeight = 20.sp,
                    )
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
            }
            if (vpnSettings != null) {
                GhostButton(
                    label = stringResource(R.string.killswitch_open_settings),
                    onClick = { runCatching { context.startActivity(vpnSettings) } },
                )
            }
        }

        SettingsGroup {
            SettingsNavRow(
                icon = Icons.Outlined.PowerSettingsNew,
                title = stringResource(R.string.settings_section_startup),
                onClick = onOpenStartup,
                summary = summaryText(SettingsSummary.startup(prefs)),
            )
            InfoNote(text = stringResource(R.string.kill_switch_android_lockdown_note))
        }
    }
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
            modifier = Modifier.padding(top = 2.dp).size(16.dp),
            tint = if (active) StateConnected else MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = text,
            modifier = Modifier.weight(1f),
            color = if (active) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
            fontSize = 14.sp,
            lineHeight = 20.sp,
        )
        if (value != null) {
            Text(
                text = value,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontFamily = FontFamily.Monospace,
                fontSize = 13.sp,
                lineHeight = 20.sp,
            )
        }
    }
}
