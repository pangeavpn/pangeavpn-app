package org.pangeavpn.app

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.SystemBarStyle
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.LaunchedEffect
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppLocale
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PanicWiper
import org.pangeavpn.core.ThemeStore
import org.pangeavpn.core.connection.HeroRules
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.isDark
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.ui.theme.LocalAppearance
import org.pangeavpn.ui.theme.PangeaTheme

private const val KEY_NOTIFICATIONS_ASKED = "notificationsAsked"

// Navigation icons stay light below API 26, so a light theme needs this behind them there.
private val legacyNavScrim = Color.argb(0x80, 0x1B, 0x1B, 0x1B)

class MainActivity : ComponentActivity() {
    private val consentLauncher = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        ConnectionRepository.onConsentResult(it.resultCode == Activity.RESULT_OK)
    }

    private val notificationLauncher = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase))
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        applyBars(initialDark())
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) setRecentsScreenshotEnabled(false)
        ConnectionRepository.init(this)
        if (Panic.requested(this, intent)) Panic.wipeNow(this) else Panic.syncShortcut(this)
        // A restore beside a wipe could write back what the wipe just cleared, so it waits for the wipe.
        lifecycleScope.launch {
            PanicWiper.wiping.first { !it }
            SessionRepository.start(this@MainActivity)
        }
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                launch { forwardConsent() }
                launch { askNotificationsAfterFirstConnect() }
            }
        }
        setContent {
            PangeaTheme {
                val dark = LocalAppearance.current.dark
                LaunchedEffect(dark) { applyBars(dark) }
                AppRoot()
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        if (Panic.requested(this, intent)) Panic.wipeNow(this)
    }

    override fun onStart() {
        super.onStart()
        AppPrefs.appInForeground = true
    }

    override fun onResume() {
        super.onResume()
        SessionRepository.onForeground()
    }

    override fun onStop() {
        AppPrefs.appInForeground = false
        super.onStop()
    }

    private fun applyBars(dark: Boolean) {
        val style = if (dark) SystemBarStyle.dark(Color.TRANSPARENT) else SystemBarStyle.light(Color.TRANSPARENT, legacyNavScrim)
        enableEdgeToEdge(style, style)
    }

    private fun initialDark(): Boolean {
        val night = resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK
        return ThemeStore(this).choice().isDark(night == Configuration.UI_MODE_NIGHT_YES)
    }

    private suspend fun forwardConsent() {
        ConnectionRepository.consentRequest.filterNotNull().collect { request ->
            try {
                consentLauncher.launch(request)
                ConnectionRepository.consentLaunched()
            } catch (e: ActivityNotFoundException) {
                ConnectionRepository.onConsentResult(false)
            }
        }
    }

    // Once per install, as the first connect with VPN consent starts: never beside the consent dialog itself.
    private suspend fun askNotificationsAfterFirstConnect() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val prefs = shellPrefs(this)
        var previous = ConnectionRepository.status.value.state
        ConnectionRepository.status.collect { status ->
            val started = HeroRules.isUp(status.state) &&
                (previous == ConnState.DISCONNECTED || previous == ConnState.ERROR)
            previous = status.state
            if (!started || prefs.getBoolean(KEY_NOTIFICATIONS_ASKED, false)) return@collect
            prefs.edit().putBoolean(KEY_NOTIFICATIONS_ASKED, true).apply()
            val permission = Manifest.permission.POST_NOTIFICATIONS
            if (checkSelfPermission(permission) != PackageManager.PERMISSION_GRANTED) notificationLauncher.launch(permission)
        }
    }
}
