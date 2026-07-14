package org.pangeavpn.core.viewmodel

import android.app.Application
import android.content.Intent
import android.provider.Settings
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.util.runCatchingCancellable

data class SettingsUiState(
    val killswitchGuideShown: Boolean = false,
    val signedOut: Boolean = false,
)

class SettingsViewModel(app: Application) : AndroidViewModel(app) {
    init {
        TunnelBridge.init(app)
    }

    private val _state = MutableStateFlow(SettingsUiState())
    val state: StateFlow<SettingsUiState> = _state.asStateFlow()

    fun showKillswitchGuide() {
        _state.value = _state.value.copy(killswitchGuideShown = true)
    }

    fun dismissKillswitchGuide() {
        _state.value = _state.value.copy(killswitchGuideShown = false)
    }

    fun openVpnSettings(): Intent = Intent(Settings.ACTION_VPN_SETTINGS)

    fun signOut() {
        viewModelScope.launch {
            PangeaVpnService.disconnect(getApplication())
            runCatchingCancellable { SessionRepository.logout() }
            _state.value = _state.value.copy(signedOut = true)
        }
    }
}
