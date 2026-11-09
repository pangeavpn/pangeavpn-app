package org.pangeavpn.tv

import android.Manifest
import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppLocale
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.connection.HeroRules
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.tv.ui.TvTheme
import org.pangeavpn.ui.components.ToastHost
import org.pangeavpn.ui.theme.PangeaTheme

private const val SHELL_PREFS = "tv_shell"
private const val KEY_NOTIFICATIONS_ASKED = "notificationsAsked"

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
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) setRecentsScreenshotEnabled(false)
        ConnectionRepository.init(this)
        SessionRepository.start(this)
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                launch { forwardConsent() }
                launch { askNotificationsAfterFirstConnect() }
            }
        }
        setContent {
            // Android TV reports no dark preference, so System means dark here.
            PangeaTheme(systemInDark = true) {
                TvTheme {
                    Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.background)) {
                        TvRoot()
                        ToastHost(Modifier.padding(horizontal = 32.dp, vertical = 15.dp))
                    }
                }
            }
        }
    }

    // Started rather than resumed: the VPN consent dialog pauses the app but leaves it on screen.
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

    // Some television builds ship without the system's VPN consent dialog.
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

    // Once per install, as the first connect starts, as the phone app does; never beside the consent dialog.
    private suspend fun askNotificationsAfterFirstConnect() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val prefs = getSharedPreferences(SHELL_PREFS, Context.MODE_PRIVATE)
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
