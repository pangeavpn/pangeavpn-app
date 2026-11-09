package org.pangeavpn.app

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.Crossfade
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.snap
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import org.pangeavpn.app.ui.DataNoticeScreen
import org.pangeavpn.app.ui.DeviceLimitScreen
import org.pangeavpn.app.ui.LoginScreen
import org.pangeavpn.app.ui.RestoreFailedScreen
import org.pangeavpn.app.ui.SplashScreen
import org.pangeavpn.app.ui.main.MainScreen
import org.pangeavpn.app.ui.regions.RegionPickerSheet
import org.pangeavpn.app.ui.settings.AccountClipboard
import org.pangeavpn.app.ui.settings.SettingsRoute
import org.pangeavpn.app.ui.settings.SettingsScreen
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PanicWiper
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.session.DataNotice
import org.pangeavpn.core.session.RootScreen
import org.pangeavpn.core.session.rootScreen
import org.pangeavpn.ui.R as UiR
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastHost
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.reducedMotion

@Composable
fun AppRoot() {
    val restored by SessionRepository.restored.collectAsState()
    val erasing by PanicWiper.wiping.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val showDeviceLimit by SessionRepository.showDeviceLimit.collectAsState()
    val session by SessionRepository.session.collectAsState()
    val restoreFailed by SessionRepository.restoreFailed.collectAsState()
    val forcedSignOut by SessionRepository.forcedSignOut.collectAsState()
    val connection by ConnectionRepository.state.collectAsState()
    val signedIn = session != null
    val screen = rootScreen(restored, erasing, DataNotice.isAccepted(prefs), showDeviceLimit, signedIn, restoreFailed)

    // Settings belong to the screen that opened them, so a sign-out or wipe closes them with it.
    var settingsFrom by remember { mutableStateOf<RootScreen?>(null) }
    var settingsRoute by remember { mutableStateOf<SettingsRoute?>(null) }
    var showRegions by remember { mutableStateOf(false) }
    val settingsShown = settingsFrom != null && settingsFrom == screen
    val reduced = reducedMotion()

    LaunchedEffect(screen) {
        if (settingsFrom != screen) settingsFrom = null
        if (screen != RootScreen.MAIN) showRegions = false
    }
    // Held apart from the flag, which a pick resets at once and would cut the sheet's slide-out.
    LaunchedEffect(connection.showServerSheet, screen) {
        if (connection.showServerSheet && screen == RootScreen.MAIN) {
            showRegions = true
            ConnectionRepository.setServerSheetShown(false)
        }
    }
    // A copied account number that fell due while in the background is cleared on return.
    val context = LocalContext.current
    val focused = LocalWindowInfo.current.isWindowFocused
    LaunchedEffect(focused) { if (focused) AccountClipboard.sweep(context) }
    val signedOutText = stringResource(UiR.string.session_signed_out)
    LaunchedEffect(forcedSignOut) {
        if (!forcedSignOut) return@LaunchedEffect
        ToastCenter.show(signedOutText)
        SessionRepository.consumeForcedSignOut()
    }

    val openSettings = { route: SettingsRoute? ->
        settingsRoute = route
        settingsFrom = screen
    }
    val fade: FiniteAnimationSpec<Float> = if (reduced) snap() else Motion.screen()

    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
        Crossfade(
            targetState = screen,
            modifier = if (settingsShown) Modifier.clearAndSetSemantics {} else Modifier,
            animationSpec = fade,
            label = "root",
        ) { shown ->
            when (shown) {
                RootScreen.SPLASH -> SplashScreen()
                RootScreen.DATA_NOTICE -> DataNoticeScreen(review = false, onDone = {})
                RootScreen.DEVICE_LIMIT -> DeviceLimitScreen()
                RootScreen.RESTORE_FAILED -> RestoreFailedScreen()
                RootScreen.LOGIN -> LoginScreen(onOpenSettings = { openSettings(null) })
                RootScreen.MAIN -> MainScreen(onOpenSettings = openSettings, onOpenRegions = { showRegions = true })
            }
        }
        AnimatedVisibility(
            visible = settingsShown,
            enter = if (reduced) EnterTransition.None else fadeIn(Motion.screen()) + slideInVertically(Motion.screen()) { it / 12 },
            exit = if (reduced) ExitTransition.None else fadeOut(Motion.screen()) + slideOutVertically(Motion.screen()) { it / 12 },
            label = "settings",
        ) {
            // Taps on the overlay's bare background must not reach the screen beneath it.
            Box(Modifier.fillMaxSize().pointerInput(Unit) {}) {
                SettingsScreen(signedIn = signedIn, initialRoute = settingsRoute, onClose = { settingsFrom = null })
            }
        }
        if (showRegions && screen == RootScreen.MAIN) {
            RegionPickerSheet(
                onDismiss = {
                    showRegions = false
                    ConnectionRepository.setServerSheetShown(false)
                },
            )
        }
        ToastHost()
    }
}
