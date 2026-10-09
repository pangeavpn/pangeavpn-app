package org.pangeavpn.app

import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.app.ui.DeviceLimitScreen
import org.pangeavpn.app.ui.LoginScreen
import org.pangeavpn.app.ui.MainScreen
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.core.viewmodel.LoginViewModel

private fun isDeviceLimitError(message: String): Boolean =
    message.contains("device limit", ignoreCase = true) || message.contains("DEVICE_LIMIT", ignoreCase = true)

/** Manual sealed-screen nav: Login -> (DeviceLimit) -> Main. Routing off Login/Main is driven by
 * [SessionRepository.session] rather than [LoginViewModel]'s own `loggedIn` flag, since nothing in
 * LoginViewModel resets that flag on sign-out — SessionRepository is the one place both login and
 * sign-out (via SettingsViewModel) agree on. */
@Composable
fun AppRoot(
    connectionViewModel: ConnectionViewModel,
    onConnectRequested: () -> Unit,
) {
    val loginViewModel: LoginViewModel = viewModel()
    val loginState by loginViewModel.state.collectAsState()
    val session by SessionRepository.session.collectAsState()

    var token by remember { mutableStateOf("") }
    var manualDeviceLimit by remember { mutableStateOf(false) }

    val showDeviceLimit = manualDeviceLimit || loginState.error?.let(::isDeviceLimitError) == true

    when {
        session != null -> MainScreen(
            connectionViewModel = connectionViewModel,
            onConnectRequested = onConnectRequested,
        )
        showDeviceLimit -> DeviceLimitScreen(
            onRetry = {
                manualDeviceLimit = false
                loginViewModel.login(token)
            },
            onBack = {
                manualDeviceLimit = false
                loginViewModel.clearError()
            },
        )
        else -> LoginScreen(
            token = token,
            onTokenChange = { token = it },
            uiState = loginState,
            onSignIn = { loginViewModel.login(token) },
            onManageDevices = { manualDeviceLimit = true },
        )
    }
}
