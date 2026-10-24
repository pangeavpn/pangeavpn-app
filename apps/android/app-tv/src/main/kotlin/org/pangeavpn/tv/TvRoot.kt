package org.pangeavpn.tv

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.core.viewmodel.ConnectionViewModel
import org.pangeavpn.core.viewmodel.LoginViewModel
import org.pangeavpn.core.viewmodel.SettingsViewModel
import org.pangeavpn.tv.ui.LoginScreen
import org.pangeavpn.tv.ui.MainScreen

/**
 * Gates LoginScreen vs MainScreen on LoginViewModel.uiState.loggedIn.
 *
 * [authEpoch] keys LoginViewModel/SettingsViewModel so sign-out gets fresh instances
 * (StateFlow won't re-emit an unchanged `true`, so the same instances can't be reused
 * to detect a second login after sign-out within one process lifetime).
 */
@Composable
fun TvRoot(connectionViewModel: ConnectionViewModel) {
    var authEpoch by rememberSaveable { mutableStateOf(0) }

    val loginViewModel: LoginViewModel = viewModel(key = "login-$authEpoch")
    val settingsViewModel: SettingsViewModel = viewModel(key = "settings-$authEpoch")

    val loginState by loginViewModel.state.collectAsState()
    val settingsState by settingsViewModel.state.collectAsState()

    LaunchedEffect(settingsState.signedOut) {
        if (settingsState.signedOut) authEpoch++
    }

    Surface(modifier = Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        if (loginState.loggedIn) {
            MainScreen(connectionViewModel = connectionViewModel, settingsViewModel = settingsViewModel)
        } else {
            LoginScreen(loginViewModel = loginViewModel)
        }
    }
}
