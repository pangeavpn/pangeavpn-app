package org.pangeavpn.app.ui.settings

import android.os.SystemClock
import androidx.activity.compose.BackHandler
import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.animation.togetherWith
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.app.ui.settings.pages.AppearancePage
import org.pangeavpn.app.ui.settings.pages.KillSwitchPage
import org.pangeavpn.app.ui.settings.pages.LanguagePage
import org.pangeavpn.app.ui.settings.pages.NetworkPage
import org.pangeavpn.app.ui.settings.pages.NotificationsPage
import org.pangeavpn.app.ui.settings.pages.PanicPage
import org.pangeavpn.app.ui.settings.pages.ProvisioningPage
import org.pangeavpn.app.ui.settings.pages.SplitTunnelPage
import org.pangeavpn.app.ui.settings.pages.StartupPage
import org.pangeavpn.app.ui.settings.pages.TransportPage
import org.pangeavpn.core.AppLocale
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.SplitTunnelStore
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.settings.SettingsSummary
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ConfirmDialog
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.SettingsNavRow
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastHost
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.displayedLanguageTag
import org.pangeavpn.ui.text.subscriptionLineText
import org.pangeavpn.ui.text.summaryText
import org.pangeavpn.ui.theme.LocalAppearance
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.StateError
import org.pangeavpn.ui.theme.accentTitle
import org.pangeavpn.ui.theme.reducedMotion

private val WideMinWidth = 700.dp
private val RailWidth = 320.dp
private val HomeMaxWidth = 560.dp
private val RailRowShape = RoundedCornerShape(10.dp)

// Sign-out and delete run to the end and report even once settings has closed.
private val accountScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

private val PathSaver = Saver<List<SettingsRoute>, String>(
    save = { path -> path.joinToString(",") { it.name } },
    restore = { saved -> saved.split(',').filter { it.isNotEmpty() }.map { SettingsRoute.valueOf(it) } },
)

/** Settings: the home list and its pages one screen at a time, or from 700 dp a 320 dp home rail beside
 *  the selected page. signedIn (fixed at opening) adds the account card and the account actions. */
@Composable
fun SettingsScreen(signedIn: Boolean, initialRoute: SettingsRoute?, onClose: () -> Unit) {
    // Fixed at opening, so signing out from here does not reflow the page as it closes.
    val account = remember { signedIn }
    var path by rememberSaveable(stateSaver = PathSaver) { mutableStateOf(listOfNotNull(initialRoute)) }
    var refreshing by remember { mutableStateOf(account) }
    val prefs by AppPrefs.state.collectAsState()
    val context = LocalContext.current
    val focused = LocalWindowInfo.current.isWindowFocused
    val homeScroll = rememberScrollState()
    val railScroll = rememberScrollState()

    LaunchedEffect(Unit) {
        launch { SettingsRepository.load() }
        if (account) {
            SessionRepository.refreshSubscription()
            refreshing = false
        }
    }
    LaunchedEffect(prefs.developerMode) {
        if (!prefs.developerMode) path = SettingsRoute.withoutDeveloper(path)
    }
    LaunchedEffect(focused) {
        if (focused) AccountClipboard.sweep(context)
    }

    val push: (SettingsRoute) -> Unit = { route -> path = path.ifEmpty { listOf(SettingsRoute.selected(path)) } + route }
    val select: (SettingsRoute) -> Unit = { route -> path = listOf(route) }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        BoxWithConstraints(modifier = Modifier.fillMaxSize()) {
            val wide = maxWidth >= WideMinWidth

            // A wide window always shows a page, so there the last one closes settings instead.
            fun back() {
                if (path.size > 1 || (path.size == 1 && !wide)) path = path.dropLast(1) else onClose()
            }
            BackHandler { back() }

            if (wide) {
                val railWidth = (RailWidth * LocalDensity.current.fontScale).coerceIn(RailWidth, maxWidth / 2)
                Row(modifier = Modifier.fillMaxSize()) {
                    Column(
                        modifier = Modifier
                            .width(railWidth)
                            .fillMaxHeight()
                            .background(MaterialTheme.colorScheme.surface)
                            .windowInsetsPadding(
                                WindowInsets.safeDrawing.only(
                                    WindowInsetsSides.Top + WindowInsetsSides.Start + WindowInsetsSides.Bottom,
                                ),
                            ),
                    ) {
                        Header(inset = 12.dp, onClose = onClose)
                        Column(
                            modifier = Modifier
                                .weight(1f)
                                .verticalScroll(railScroll)
                                .padding(start = 12.dp, end = 12.dp, bottom = 16.dp),
                        ) {
                            HomeList(
                                rail = true,
                                account = account,
                                refreshing = refreshing,
                                selected = SettingsRoute.selected(path),
                                onOpen = select,
                                onClose = onClose,
                            )
                        }
                    }
                    Box(
                        modifier = Modifier
                            .width(1.dp)
                            .fillMaxHeight()
                            .background(MaterialTheme.colorScheme.outlineVariant),
                    )
                    Box(
                        modifier = Modifier
                            .weight(1f)
                            .fillMaxHeight()
                            .consumeWindowInsets(WindowInsets.safeDrawing.only(WindowInsetsSides.Start)),
                    ) {
                        Page(route = path.lastOrNull() ?: SettingsRoute.selected(path), onBack = { back() }, onOpen = push)
                    }
                }
            } else {
                val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl
                val reduced = reducedMotion()
                AnimatedContent(
                    targetState = path,
                    transitionSpec = {
                        val sign = if ((targetState.size >= initialState.size) != rtl) 1 else -1
                        if (reduced) {
                            fadeIn(Motion.quick()) togetherWith fadeOut(Motion.quick())
                        } else {
                            (slideInHorizontally(Motion.screen()) { sign * it / 4 } + fadeIn(Motion.screen())) togetherWith
                                (slideOutHorizontally(Motion.screen()) { -sign * it / 4 } + fadeOut(Motion.screen()))
                        }
                    },
                    contentKey = { it.lastOrNull() },
                    label = "settings",
                ) { shown ->
                    val top = shown.lastOrNull()
                    if (top == null) {
                        Column(
                            modifier = Modifier
                                .fillMaxSize()
                                .background(MaterialTheme.colorScheme.background)
                                .windowInsetsPadding(WindowInsets.safeDrawing),
                        ) {
                            Header(inset = 16.dp, onClose = onClose)
                            Column(
                                modifier = Modifier
                                    .weight(1f)
                                    .fillMaxWidth()
                                    .verticalScroll(homeScroll)
                                    .wrapContentWidth()
                                    .widthIn(max = HomeMaxWidth)
                                    .fillMaxWidth()
                                    .padding(start = 16.dp, end = 16.dp, bottom = 20.dp),
                            ) {
                                HomeList(
                                    rail = false,
                                    account = account,
                                    refreshing = refreshing,
                                    selected = null,
                                    onOpen = select,
                                    onClose = onClose,
                                )
                            }
                        }
                    } else {
                        Page(route = top, onBack = { back() }, onOpen = push)
                    }
                }
            }
        }
        ToastHost()
    }
}

@Composable
private fun Page(route: SettingsRoute, onBack: () -> Unit, onOpen: (SettingsRoute) -> Unit) {
    when (route) {
        SettingsRoute.TRANSPORT -> TransportPage(onBack = onBack)
        SettingsRoute.PROVISIONING -> ProvisioningPage(onBack = onBack)
        SettingsRoute.NETWORK -> NetworkPage(onBack = onBack)
        SettingsRoute.KILL_SWITCH -> KillSwitchPage(onBack = onBack, onOpenStartup = { onOpen(SettingsRoute.STARTUP) })
        SettingsRoute.SPLIT_TUNNEL -> SplitTunnelPage(onBack = onBack)
        SettingsRoute.STARTUP -> StartupPage(onBack = onBack)
        SettingsRoute.NOTIFICATIONS -> NotificationsPage(onBack = onBack)
        SettingsRoute.PANIC -> PanicPage(onBack = onBack)
        SettingsRoute.LANGUAGE -> LanguagePage(onBack = onBack)
        SettingsRoute.APPEARANCE -> AppearancePage(onBack = onBack)
        SettingsRoute.SUPPORT -> SupportPage(onBack = onBack)
        SettingsRoute.UPDATE -> UpdatePage(onBack = onBack)
        SettingsRoute.DEVELOPER -> DeveloperPage(onBack = onBack)
        SettingsRoute.DEVICES -> DevicesPage(onBack = onBack)
        SettingsRoute.SUBSCRIPTION -> SubscriptionPage(onBack = onBack)
    }
}

@Composable
private fun Header(inset: Dp, onClose: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 56.dp)
            .padding(start = inset, end = inset - 4.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.settings_title),
            modifier = Modifier.weight(1f).semantics { heading() },
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 28.sp,
            fontWeight = FontWeight.Light,
            letterSpacing = (-0.6).sp,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        IconAction(
            icon = Icons.Rounded.Close,
            label = stringResource(R.string.settings_close),
            onClick = onClose,
            tint = MaterialTheme.colorScheme.onSurface,
        )
    }
}

/** The home's content, as a phone's list or as the wide window's rail; emitted into the caller's column. */
@Composable
private fun HomeList(
    rail: Boolean,
    account: Boolean,
    refreshing: Boolean,
    selected: SettingsRoute?,
    onOpen: (SettingsRoute) -> Unit,
    onClose: () -> Unit,
) {
    val prefs by AppPrefs.state.collectAsState()
    val summaries = summaries()
    val gap = if (rail) 2.dp else 8.dp

    if (account) {
        AccountCard(
            refreshing = refreshing,
            fill = if (rail) MaterialTheme.colorScheme.background else MaterialTheme.colorScheme.surface,
        )
    }
    Column(
        modifier = Modifier.padding(top = if (rail) (if (account) 14.dp else 4.dp) else (if (account) 20.dp else 8.dp)),
        verticalArrangement = Arrangement.spacedBy(gap),
    ) {
        SettingsRoute.home.filter { it != SettingsRoute.DEVELOPER || prefs.developerMode }.forEach { route ->
            Entry(
                rail = rail,
                icon = route.icon,
                title = route.title(),
                summary = summaries[route],
                onClick = { onOpen(route) },
                selected = route == selected,
            )
        }
    }
    if (account) {
        AccountActions(
            rail = rail,
            selected = selected,
            summaries = summaries,
            onOpen = onOpen,
            onSignedOut = onClose,
            modifier = Modifier.padding(top = if (rail) 12.dp else 20.dp),
        )
    }
    VersionLine(modifier = Modifier.padding(top = if (rail) 4.dp else 12.dp))
}

@Composable
private fun summaries(): Map<SettingsRoute, String> {
    val settings by SettingsRepository.settings.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val excludedApps by SplitTunnelStore.excludedApps.collectAsState()
    val systemVpn by PangeaVpnService.systemVpnState.collectAsState()
    val subscription by SessionRepository.subscription.collectAsState()
    val context = LocalContext.current
    // A language change reconfigures the activity, so the pick is read once per configuration.
    val picked = remember(LocalConfiguration.current) { AppLocale.current(context) }
    val language = picked ?: displayedLanguageTag()
    return mapOf(
        SettingsRoute.TRANSPORT to summaryText(SettingsSummary.transport(settings)),
        SettingsRoute.PROVISIONING to summaryText(SettingsSummary.provisioning(settings)),
        SettingsRoute.NETWORK to summaryText(SettingsSummary.network(settings)),
        SettingsRoute.KILL_SWITCH to summaryText(SettingsSummary.killSwitch(strict = systemVpn?.blockWithoutVpn == true)),
        SettingsRoute.SPLIT_TUNNEL to summaryText(SettingsSummary.splitTunnel(prefs, excludedApps.size)),
        SettingsRoute.STARTUP to summaryText(SettingsSummary.startup(prefs)),
        SettingsRoute.NOTIFICATIONS to summaryText(SettingsSummary.notifications(prefs)),
        SettingsRoute.PANIC to summaryText(SettingsSummary.panic(prefs)),
        SettingsRoute.LANGUAGE to summaryText(SettingsSummary.language(language)),
        SettingsRoute.APPEARANCE to accentTitle(LocalAppearance.current.accent),
        SettingsRoute.UPDATE to summaryText(SettingsSummary.update(BuildConfig.VERSION_NAME)),
        SettingsRoute.DEVELOPER to summaryText(SettingsSummary.developer(prefs)),
        SettingsRoute.SUBSCRIPTION to subscriptionLineText(subscription).text,
    )
}

@Composable
private fun AccountActions(
    rail: Boolean,
    selected: SettingsRoute?,
    summaries: Map<SettingsRoute, String>,
    onOpen: (SettingsRoute) -> Unit,
    onSignedOut: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val signingOut by SessionRepository.signingOut.collectAsState()
    val deleting by SessionRepository.deletingAccount.collectAsState()
    var confirmingSignOut by remember { mutableStateOf(false) }
    var confirmingDelete by remember { mutableStateOf(false) }
    val signOutTitle = stringResource(R.string.settings_signout)
    val deleteTitle = stringResource(R.string.settings_delete_account)
    val deletedText = stringResource(R.string.settings_delete_done)
    val deleteFailedText = stringResource(R.string.settings_delete_failed)

    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(if (rail) 2.dp else 8.dp)) {
        if (rail) {
            Box(
                modifier = Modifier
                    .padding(bottom = 10.dp)
                    .fillMaxWidth()
                    .height(1.dp)
                    .background(MaterialTheme.colorScheme.outlineVariant),
            )
        }
        SettingsRoute.account.forEach { route ->
            Entry(
                rail = rail,
                icon = route.icon,
                title = route.title(),
                summary = summaries[route],
                onClick = { onOpen(route) },
                selected = route == selected,
            )
        }
        Entry(
            rail = rail,
            icon = Icons.AutoMirrored.Outlined.Logout,
            title = signOutTitle,
            summary = null,
            onClick = { if (!deleting) confirmingSignOut = true },
            tint = StateError,
            loading = signingOut,
            chevron = false,
        )
        Entry(
            rail = rail,
            icon = Icons.Outlined.Delete,
            title = deleteTitle,
            summary = null,
            onClick = { if (!signingOut) confirmingDelete = true },
            tint = StateError,
            loading = deleting,
            chevron = false,
        )
    }

    if (confirmingSignOut) {
        ConfirmDialog(
            title = stringResource(R.string.settings_home_signout_title),
            message = stringResource(R.string.settings_home_signout_body),
            confirmLabel = signOutTitle,
            onConfirm = {
                accountScope.launch {
                    SessionRepository.signOut()
                    onSignedOut()
                }
            },
            onDismiss = { confirmingSignOut = false },
            destructive = true,
        )
    }
    // Deleting clears the session, which also takes the app back to sign-in.
    if (confirmingDelete) {
        ConfirmDialog(
            title = stringResource(R.string.settings_delete_title),
            message = stringResource(R.string.settings_delete_body_android),
            confirmLabel = deleteTitle,
            onConfirm = {
                accountScope.launch {
                    val deleted = SessionRepository.deleteAccount()
                    if (deleted) {
                        ToastCenter.show(deletedText, ToastStyle.SUCCESS)
                    } else {
                        ToastCenter.show(deleteFailedText, ToastStyle.WARNING)
                    }
                    if (SessionRepository.session.value == null) onSignedOut()
                }
            },
            onDismiss = { confirmingDelete = false },
            destructive = true,
        )
    }
}

@Composable
private fun Entry(
    rail: Boolean,
    icon: ImageVector,
    title: String,
    summary: String?,
    onClick: () -> Unit,
    selected: Boolean = false,
    tint: Color? = null,
    loading: Boolean = false,
    chevron: Boolean = true,
) {
    if (rail) {
        RailRow(icon = icon, title = title, summary = summary, onClick = onClick, selected = selected, tint = tint, loading = loading)
    } else {
        SettingsNavRow(
            icon = icon,
            title = title,
            onClick = onClick,
            summary = summary,
            tint = tint,
            showChevron = chevron,
            loading = loading,
        )
    }
}

/** Desktop's rail item: flat until selected, then washed in the accent with its icon and summary tinted. */
@Composable
private fun RailRow(
    icon: ImageVector,
    title: String,
    summary: String?,
    onClick: () -> Unit,
    selected: Boolean,
    tint: Color?,
    loading: Boolean,
) {
    val accent = MaterialTheme.colorScheme.primary
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .alpha(if (loading) 0.5f else 1f)
            .clip(RailRowShape)
            .background(if (selected) accent.copy(alpha = 0.15f) else Color.Transparent)
            .clickable(role = Role.Button) { if (!loading) onClick() }
            .semantics { this.selected = selected }
            .heightIn(min = 48.dp)
            .padding(horizontal = 10.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.size(20.dp),
            tint = tint ?: if (selected) accent else LocalTextSecondary.current,
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                color = tint ?: MaterialTheme.colorScheme.onSurface,
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
            )
            if (!summary.isNullOrEmpty()) {
                Text(
                    text = summary,
                    modifier = Modifier.padding(top = 2.dp),
                    color = if (selected) accent else MaterialTheme.colorScheme.onSurfaceVariant,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 12.sp,
                    lineHeight = 16.sp,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(18.dp),
                color = tint ?: MaterialTheme.colorScheme.onSurfaceVariant,
                strokeWidth = 2.dp,
            )
        }
    }
}

@Composable
private fun VersionLine(modifier: Modifier = Modifier) {
    var taps by remember { mutableStateOf(VersionTapUnlock()) }
    val onText = stringResource(R.string.settings_home_developer_on)
    val offText = stringResource(R.string.settings_home_developer_off)
    Text(
        text = stringResource(R.string.settings_home_version, BuildConfig.VERSION_NAME, BuildConfig.VERSION_CODE.toString()),
        modifier = modifier
            .fillMaxWidth()
            .clickable(interactionSource = remember { MutableInteractionSource() }, indication = null) {
                val next = taps.tapped(SystemClock.elapsedRealtime())
                taps = next
                if (next.fired) {
                    val enabled = !AppPrefs.value.developerMode
                    AppPrefs.update { it.copy(developerMode = enabled) }
                    ToastCenter.show(if (enabled) onText else offText)
                }
            }
            .heightIn(min = 48.dp)
            .wrapContentHeight(),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontFamily = FontFamily.Monospace,
        fontSize = 12.sp,
        textAlign = TextAlign.Center,
    )
}

/** Desktop's hidden switch: five taps on the version, each within 1.5 s of the last. */
private data class VersionTapUnlock(val count: Int = 0, val lastTapMs: Long? = null, val fired: Boolean = false) {
    fun tapped(nowMs: Long): VersionTapUnlock {
        val gap = lastTapMs?.let { nowMs - it }
        val next = if (gap != null && gap in 0..WINDOW_MS) count + 1 else 1
        return if (next >= TAPS_NEEDED) VersionTapUnlock(fired = true) else VersionTapUnlock(next, nowMs)
    }

    companion object {
        const val TAPS_NEEDED = 5
        const val WINDOW_MS = 1_500L
    }
}
