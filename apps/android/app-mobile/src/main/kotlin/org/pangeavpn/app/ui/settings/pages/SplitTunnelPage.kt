package org.pangeavpn.app.ui.settings.pages

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Wifi
import androidx.compose.material.icons.rounded.Apps
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.LayoutDirection
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.SplitTunnelStore
import org.pangeavpn.core.settings.RangesParse
import org.pangeavpn.core.settings.SplitTunnelRanges
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.CommittedField
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsNavRow
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.StoreSwitchRow
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.rangesErrorText

@Composable
fun SplitTunnelPage(onBack: () -> Unit) {
    val error = networkPageError()
    var showApps by rememberSaveable { mutableStateOf(false) }

    if (showApps) {
        // Composed only while the list shows, so it registers after the settings shell's own back handler.
        BackHandler { showApps = false }
        SplitTunnelAppList(onBack = { showApps = false })
    } else {
        SplitTunnelSettings(error = error, onBack = onBack, onOpenApps = { showApps = true })
    }
}

@Composable
private fun SplitTunnelSettings(error: String?, onBack: () -> Unit, onOpenApps: () -> Unit) {
    val prefs by AppPrefs.state.collectAsState()
    val excludedApps by SplitTunnelStore.excludedApps.collectAsState()
    val resources = LocalContext.current.resources
    var refused by remember { mutableStateOf<RangesParse.Refused?>(null) }
    // Bumped on each save, so the field drops what was typed and shows the stored, normalized list.
    var revision by remember { mutableIntStateOf(0) }
    val rangesTitle = stringResource(R.string.split_tunnel_ranges_title)

    fun commitRanges(text: String) {
        when (val parsed = SplitTunnelRanges.parse(text)) {
            is RangesParse.Ranges -> {
                refused = null
                AppPrefs.update { it.copy(splitTunnelRanges = parsed.ranges) }
                revision++
                if (parsed.ranges.isEmpty()) {
                    ToastCenter.show(resources.getString(R.string.split_tunnel_toast_cleared), ToastStyle.INFO)
                } else {
                    ToastCenter.show(resources.getString(R.string.split_tunnel_toast_saved), ToastStyle.SUCCESS)
                }
            }
            is RangesParse.Refused -> refused = parsed
        }
    }

    SettingsPage(
        title = stringResource(R.string.settings_section_split_tunnel),
        onBack = onBack,
        description = stringResource(R.string.split_tunnel_page_description),
        error = error,
    ) {
        SettingsGroup {
            StoreSwitchRow(
                title = stringResource(R.string.split_tunnel_enabled_title),
                value = prefs.splitTunnelEnabled,
                save = { enabled ->
                    if (enabled != AppPrefs.value.splitTunnelEnabled) {
                        AppPrefs.update { it.copy(splitTunnelEnabled = enabled) }
                        if (enabled) {
                            ToastCenter.show(resources.getString(R.string.split_tunnel_toast_on), ToastStyle.SUCCESS)
                        } else {
                            ToastCenter.show(resources.getString(R.string.split_tunnel_toast_off), ToastStyle.INFO)
                        }
                    }
                },
                hint = stringResource(R.string.split_tunnel_enabled_hint_apps),
            )
        }
        SettingsGroup {
            SettingsNavRow(
                icon = Icons.Rounded.Apps,
                title = stringResource(R.string.split_tunnel_apps_title),
                onClick = onOpenApps,
                summary = pluralStringResource(R.plurals.split_tunnel_summary_apps, excludedApps.size, excludedApps.size),
            )
        }
        SettingsGroup(title = rangesTitle) {
            key(revision) {
                // Addresses read left to right in every language, so the field does too.
                CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
                    CommittedField(
                        label = rangesTitle,
                        initial = SplitTunnelRanges.text(prefs.splitTunnelRanges),
                        onCommit = { commitRanges(it) },
                        placeholder = stringResource(R.string.split_tunnel_ranges_placeholder),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                    )
                }
            }
            NetworkHint(stringResource(R.string.split_tunnel_ranges_hint))
            refused?.let { InfoNote(text = rangesErrorText(it), tone = NoteTone.ERROR) }
        }
        SettingsGroup {
            InfoNote(text = stringResource(R.string.split_tunnel_note_dns), icon = Icons.Outlined.Info)
            InfoNote(text = stringResource(R.string.split_tunnel_note_lan), icon = Icons.Outlined.Wifi)
            InfoNote(text = stringResource(R.string.split_tunnel_note_restart), icon = Icons.Outlined.Refresh)
        }
    }
}
