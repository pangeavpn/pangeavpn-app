@file:OptIn(ExperimentalComposeUiApi::class)

package org.pangeavpn.tv.ui.settings

import android.content.Context
import androidx.activity.compose.BackHandler
import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusGroup
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.CallSplit
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.Security
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.SupportAgent
import androidx.compose.material.icons.outlined.Translate
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshots.SnapshotStateMap
import androidx.compose.ui.Alignment
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.pm.PackageInfoCompat
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.SplitTunnelStore
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.diagnostics.DiagnosticsStore
import org.pangeavpn.core.model.HubTestResult
import org.pangeavpn.core.settings.SettingsSummary
import org.pangeavpn.core.viewmodel.DevicesViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ToastHost
import org.pangeavpn.ui.text.displayedLanguageTag
import org.pangeavpn.ui.text.subscriptionLineText
import org.pangeavpn.ui.text.summaryText
import org.pangeavpn.ui.theme.LocalAppearance
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.accentTitle

internal enum class TvSection(@StringRes val titleRes: Int, @StringRes val descriptionRes: Int?, val icon: ImageVector) {
    ACCOUNT(R.string.tv_settings_account, null, Icons.Outlined.AccountCircle),
    TRANSPORT(R.string.settings_transport_heading, R.string.settings_transport_description, Icons.Outlined.Shield),
    PROVISIONING(R.string.settings_censorship_heading, R.string.settings_censorship_description, Icons.Outlined.Dns),
    NETWORK(R.string.settings_network_heading, R.string.settings_network_description, Icons.Outlined.Public),
    KILL_SWITCH(R.string.settings_section_kill_switch, R.string.kill_switch_description, Icons.Outlined.Security),
    SPLIT_TUNNEL(
        R.string.settings_section_split_tunnel,
        R.string.split_tunnel_page_description,
        Icons.AutoMirrored.Outlined.CallSplit,
    ),
    STARTUP(R.string.settings_section_startup, R.string.startup_description, Icons.Outlined.Bolt),
    LANGUAGE(R.string.settings_section_language, R.string.language_description, Icons.Outlined.Translate),
    APPEARANCE(R.string.settings_section_appearance, R.string.appearance_description, Icons.Outlined.Palette),
    SUPPORT(R.string.settings_section_support, R.string.support_description, Icons.Outlined.SupportAgent),
    ABOUT(R.string.tv_settings_about, R.string.update_description, Icons.Outlined.Info),
}

internal data class AppBuild(val version: String, val build: String)

@Suppress("DEPRECATION")
private fun appBuild(context: Context): AppBuild {
    val info = context.packageManager.getPackageInfo(context.packageName, 0)
    return AppBuild(info.versionName.orEmpty().ifEmpty { "0.0.0" }, PackageInfoCompat.getLongVersionCode(info).toString())
}

/** Settings for the remote: a rail of sections whose focus picks the page shown beside it. Back
 *  from the page returns to the rail, and Back on the rail closes the screen. */
@Composable
fun TvSettingsScreen(signedIn: Boolean, onClose: () -> Unit) {
    val context = LocalContext.current
    val sections = remember(signedIn) { if (signedIn) TvSection.entries else TvSection.entries - TvSection.ACCOUNT }
    var chosen by rememberSaveable { mutableStateOf(sections.first()) }
    val selected = if (chosen in sections) chosen else sections.first()
    val railFocus = remember { TvSection.entries.associateWith { FocusRequester() } }
    val contentFocus = remember { FocusRequester() }
    var contentFocused by remember { mutableStateOf(false) }
    val screenScope = rememberCoroutineScope()
    val hubResults = remember { mutableStateMapOf<String, HubTestResult>() }
    val devices: DevicesViewModel = viewModel()
    var refreshingSubscription by remember { mutableStateOf(false) }
    val build = remember(context) { appBuild(context) }

    LaunchedEffect(Unit) { railFocus.getValue(selected).requestFocus() }
    LaunchedEffect(Unit) { SettingsRepository.load() }
    // Once per visit, as iOS refreshes the account card and loads the device list.
    LaunchedEffect(signedIn) {
        if (!signedIn) return@LaunchedEffect
        launch { devices.load() }
        refreshingSubscription = true
        SessionRepository.refreshSubscription()
        refreshingSubscription = false
    }
    DisposableEffect(Unit) {
        onDispose { DiagnosticsStore.resetReport() }
    }

    BackHandler {
        if (contentFocused) railFocus.getValue(selected).requestFocus() else onClose()
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        // TV overscan: 5% of each edge stays clear.
        Row(modifier = Modifier.fillMaxSize().padding(horizontal = 48.dp, vertical = 27.dp)) {
            Column(modifier = Modifier.width(272.dp).fillMaxHeight()) {
                Text(
                    text = stringResource(R.string.settings_title),
                    modifier = Modifier.padding(start = 4.dp, bottom = 14.dp).semantics { heading() },
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 26.sp,
                    fontWeight = FontWeight.Light,
                    letterSpacing = (-0.4).sp,
                )
                Column(
                    modifier = Modifier
                        .weight(1f)
                        .fillMaxWidth()
                        .focusProperties {
                            enter = { railFocus.getValue(selected) }
                            exit = { direction ->
                                when (direction) {
                                    FocusDirection.Up, FocusDirection.Down -> FocusRequester.Cancel
                                    FocusDirection.Left, FocusDirection.Right -> contentFocus
                                    else -> FocusRequester.Default
                                }
                            }
                        }
                        .focusGroup()
                        .verticalScroll(rememberScrollState())
                        .padding(vertical = 4.dp),
                    verticalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    val summaries = railSummaries(refreshingSubscription, build)
                    sections.forEach { section ->
                        RailItem(
                            section = section,
                            summary = summaries[section],
                            selected = section == selected,
                            focusRequester = railFocus.getValue(section),
                            onFocused = { chosen = section },
                            onClick = { contentFocus.requestFocus() },
                        )
                    }
                }
            }
            Spacer(modifier = Modifier.width(28.dp))
            Column(modifier = Modifier.weight(1f).fillMaxHeight()) {
                Text(
                    text = stringResource(selected.titleRes),
                    modifier = Modifier.padding(start = 4.dp, top = 4.dp, bottom = 10.dp).semantics { heading() },
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 22.sp,
                    fontWeight = FontWeight.Light,
                    letterSpacing = (-0.4).sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                key(selected) {
                    Column(
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxWidth()
                            .onFocusChanged { contentFocused = it.hasFocus }
                            .focusRequester(contentFocus)
                            .focusProperties {
                                exit = { direction ->
                                    if (direction == FocusDirection.Up || direction == FocusDirection.Down) {
                                        FocusRequester.Cancel
                                    } else {
                                        FocusRequester.Default
                                    }
                                }
                            }
                            .focusGroup()
                            .verticalScroll(rememberScrollState())
                            .padding(top = 4.dp, bottom = 24.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        selected.descriptionRes?.let { TvParagraph(stringResource(it)) }
                        SectionContent(
                            section = selected,
                            devices = devices,
                            refreshingSubscription = refreshingSubscription,
                            hubResults = hubResults,
                            build = build,
                            screenScope = screenScope,
                            onClose = onClose,
                        )
                    }
                }
            }
        }
        ToastHost(modifier = Modifier.padding(horizontal = 32.dp, vertical = 15.dp))
    }
}

@Composable
private fun SectionContent(
    section: TvSection,
    devices: DevicesViewModel,
    refreshingSubscription: Boolean,
    hubResults: SnapshotStateMap<String, HubTestResult>,
    build: AppBuild,
    screenScope: CoroutineScope,
    onClose: () -> Unit,
) {
    when (section) {
        TvSection.ACCOUNT -> AccountSection(devices, refreshingSubscription, screenScope, onClose)
        TvSection.TRANSPORT -> TransportSection()
        TvSection.PROVISIONING -> ProvisioningSection(hubResults)
        TvSection.NETWORK -> NetworkSection()
        TvSection.KILL_SWITCH -> KillSwitchSection()
        TvSection.SPLIT_TUNNEL -> SplitTunnelSection()
        TvSection.STARTUP -> StartupSection()
        TvSection.LANGUAGE -> LanguageSection()
        TvSection.APPEARANCE -> AppearanceSection()
        TvSection.SUPPORT -> SupportSection()
        TvSection.ABOUT -> AboutSection(build)
    }
}

/** The one-line value under each rail item, as the iPad rail shows it. */
@Composable
private fun railSummaries(refreshingSubscription: Boolean, build: AppBuild): Map<TvSection, String> {
    val settings by SettingsRepository.settings.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val excludedApps by SplitTunnelStore.excludedApps.collectAsState()
    val systemVpn by PangeaVpnService.systemVpnState.collectAsState()
    val subscription by SessionRepository.subscription.collectAsState()
    val account =
        if (refreshingSubscription && subscription == null) stringResource(R.string.settings_account_loading)
        else subscriptionLineText(subscription).text
    return mapOf(
        TvSection.ACCOUNT to account,
        TvSection.TRANSPORT to summaryText(SettingsSummary.transport(settings)),
        TvSection.PROVISIONING to summaryText(SettingsSummary.provisioning(settings)),
        TvSection.NETWORK to summaryText(SettingsSummary.network(settings)),
        // Android's block switch, not iOS's "Strict", which names a mode this app does not have.
        TvSection.KILL_SWITCH to stringResource(
            if (systemVpn?.blockWithoutVpn == true) R.string.kill_switch_android_block_without_vpn
            else R.string.kill_switch_always_on,
        ),
        TvSection.SPLIT_TUNNEL to summaryText(SettingsSummary.splitTunnel(prefs, excludedApps.size)),
        TvSection.STARTUP to summaryText(SettingsSummary.startup(prefs)),
        TvSection.LANGUAGE to summaryText(SettingsSummary.language(displayedLanguageTag())),
        TvSection.APPEARANCE to accentTitle(LocalAppearance.current.accent),
        TvSection.ABOUT to summaryText(SettingsSummary.update(build.version)),
    )
}

@Composable
private fun RailItem(
    section: TvSection,
    summary: String?,
    selected: Boolean,
    focusRequester: FocusRequester,
    onFocused: () -> Unit,
    onClick: () -> Unit,
) {
    var focused by remember { mutableStateOf(false) }
    val colors = MaterialTheme.colorScheme
    val accent = focused || selected

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(TvRowShape)
            .background(
                when {
                    focused -> colors.primary.copy(alpha = 0.22f)
                    selected -> colors.surface
                    else -> Color.Transparent
                },
            )
            .border(width = 2.dp, color = if (focused) colors.primary else Color.Transparent, shape = TvRowShape)
            .focusRequester(focusRequester)
            .onFocusChanged {
                focused = it.isFocused
                if (it.isFocused) onFocused()
            }
            .selectable(selected = selected, role = Role.Tab, onClick = onClick)
            .heightIn(min = 52.dp)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            imageVector = section.icon,
            contentDescription = null,
            modifier = Modifier.size(22.dp),
            tint = if (accent) colors.primary else LocalTextSecondary.current,
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = stringResource(section.titleRes),
                color = colors.onSurface,
                fontSize = 15.sp,
                fontWeight = if (accent) FontWeight.SemiBold else FontWeight.Medium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (!summary.isNullOrEmpty()) {
                Text(
                    text = summary,
                    color = colors.onSurfaceVariant,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 12.sp,
                    lineHeight = 16.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}
