package org.pangeavpn.app

import android.Manifest
import android.app.Activity
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.ui.theme.PangeaTheme

class MainActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            PangeaTheme {
                val context = LocalContext.current
                val connectionViewModel: ConnectionViewModel = viewModel()

                val consentLauncher = rememberLauncherForActivityResult(
                    ActivityResultContracts.StartActivityForResult(),
                ) { result ->
                    if (result.resultCode == Activity.RESULT_OK) {
                        connectionViewModel.pendingServerId?.let { connectionViewModel.connect(it) }
                    }
                }

                val notificationPermissionLauncher = rememberLauncherForActivityResult(
                    ActivityResultContracts.RequestPermission(),
                ) { }

                val consentIntent by connectionViewModel.consentIntent.collectAsState()
                LaunchedEffect(consentIntent) {
                    consentIntent?.let { intent ->
                        consentLauncher.launch(intent)
                        connectionViewModel.consentIntentConsumed()
                    }
                }

                var notificationPermissionAsked by remember { mutableStateOf(false) }

                AppRoot(
                    connectionViewModel = connectionViewModel,
                    onConnectRequested = {
                        if (!notificationPermissionAsked) {
                            notificationPermissionAsked = true
                            val needsPermission = Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
                                ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
                                PackageManager.PERMISSION_GRANTED
                            if (needsPermission) {
                                notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
                            }
                        }
                    },
                )
            }
        }
    }
}
