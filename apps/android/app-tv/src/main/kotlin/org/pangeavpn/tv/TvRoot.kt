package org.pangeavpn.tv

import androidx.activity.compose.BackHandler
import androidx.compose.animation.Crossfade
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PanicWiper
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.session.DataNotice
import org.pangeavpn.core.session.RootScreen
import org.pangeavpn.core.session.rootScreen
import org.pangeavpn.tv.ui.TvDataNoticeScreen
import org.pangeavpn.tv.ui.TvDeviceLimitScreen
import org.pangeavpn.tv.ui.TvLoginScreen
import org.pangeavpn.tv.ui.TvMainScreen
import org.pangeavpn.tv.ui.TvRestoreFailedScreen
import org.pangeavpn.tv.ui.settings.TvSettingsScreen
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.BrandLogo
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.theme.Motion

/** Picks the screen as iOS RootView does; settings replace the whole screen, as a remote has nothing to swipe. */
@Composable
fun TvRoot() {
    val restored by SessionRepository.restored.collectAsState()
    val erasing by PanicWiper.wiping.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val showDeviceLimit by SessionRepository.showDeviceLimit.collectAsState()
    val session by SessionRepository.session.collectAsState()
    val restoreFailed by SessionRepository.restoreFailed.collectAsState()
    val forcedSignOut by SessionRepository.forcedSignOut.collectAsState()
    val resources = LocalContext.current.resources

    val signedIn = session != null
    val screen = rootScreen(
        restored = restored,
        erasing = erasing,
        noticeAccepted = DataNotice.isAccepted(prefs),
        showDeviceLimit = showDeviceLimit,
        signedIn = signedIn,
        restoreFailed = restoreFailed,
    )

    // Memory only: the account number is a credential, so it never goes into saved state.
    var draftToken by remember { mutableStateOf("") }
    // Saved, so a language change that recreates the activity reopens settings where it was.
    var settingsOpen by rememberSaveable { mutableStateOf(false) }
    val settingsShown = settingsOpen && (screen == RootScreen.LOGIN || screen == RootScreen.MAIN)

    var lastSignedIn by remember { mutableStateOf(signedIn) }
    LaunchedEffect(signedIn) {
        if (signedIn == lastSignedIn) return@LaunchedEffect
        lastSignedIn = signedIn
        settingsOpen = false
        if (signedIn) draftToken = ""
    }

    LaunchedEffect(forcedSignOut) {
        if (!forcedSignOut) return@LaunchedEffect
        ToastCenter.show(resources.getString(R.string.session_signed_out), ToastStyle.WARNING)
        SessionRepository.consumeForcedSignOut()
    }

    var wasErasing by remember { mutableStateOf(false) }
    LaunchedEffect(erasing) {
        if (erasing) settingsOpen = false
        if (wasErasing && !erasing) ToastCenter.show(resources.getString(R.string.root_data_erased), ToastStyle.SUCCESS)
        wasErasing = erasing
    }

    // Declared before the settings screen, so its own page stack handles back first.
    BackHandler(enabled = settingsShown) { settingsOpen = false }

    Crossfade(
        targetState = if (settingsShown) null else screen,
        modifier = Modifier.fillMaxSize(),
        animationSpec = Motion.screen(),
        label = "root",
    ) { shown ->
        when (shown) {
            null -> TvSettingsScreen(signedIn = signedIn, onClose = { settingsOpen = false })
            RootScreen.SPLASH -> Splash()
            RootScreen.DATA_NOTICE -> TvDataNoticeScreen(review = false, onDone = { DataNotice.accept() })
            RootScreen.DEVICE_LIMIT -> TvDeviceLimitScreen()
            RootScreen.RESTORE_FAILED -> TvRestoreFailedScreen()
            RootScreen.LOGIN -> TvLoginScreen(
                token = draftToken,
                onTokenChange = { draftToken = it },
                onOpenSettings = { settingsOpen = true },
            )
            RootScreen.MAIN -> TvMainScreen(onOpenSettings = { settingsOpen = true })
        }
    }
}

@Composable
private fun Splash() {
    Box(modifier = Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        BrandLogo(size = 72.dp)
    }
}
