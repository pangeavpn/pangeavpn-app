package org.pangeavpn.tv

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.core.content.ContextCompat
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.ui.theme.PangeaTheme

/** Single Activity host. Owns the VPN consent flow: core ViewModels stay platform-agnostic,
 * so launching the system consent dialog and resuming connect() on approval happens here. */
class MainActivity : ComponentActivity() {

    private val connectionViewModel: ConnectionViewModel by viewModels()

    private val consentLauncher = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult(),
    ) { result ->
        if (result.resultCode == RESULT_OK) {
            connectionViewModel.pendingServerId?.let { connectionViewModel.connect(it) }
        }
    }

    private val notificationPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { /* non-blocking; TV rarely surfaces this prompt */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestNotificationPermissionIfNeeded()

        setContent {
            val consentIntent by connectionViewModel.consentIntent.collectAsState()
            LaunchedEffect(consentIntent) {
                consentIntent?.let {
                    consentLauncher.launch(it)
                    connectionViewModel.consentIntentConsumed()
                }
            }
            PangeaTheme(systemInDark = true) {
                TvRoot(connectionViewModel = connectionViewModel)
            }
        }
    }

    private fun requestNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val granted = ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
        if (!granted) {
            notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
}
