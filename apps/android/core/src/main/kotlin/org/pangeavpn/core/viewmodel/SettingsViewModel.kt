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
import org.pangeavpn.core.model.AppSettings
import org.pangeavpn.core.util.runCatchingCancellable

data class SettingsUiState(
    val settings: AppSettings = AppSettings(),
    val loaded: Boolean = false,
    val killswitchGuideShown: Boolean = false,
    val signedOut: Boolean = false,
    // Set when the core rejected a change, e.g. the last hub method.
    val error: String? = null,
)

class SettingsViewModel(app: Application) : AndroidViewModel(app) {
    init {
        TunnelBridge.init(app)
    }

    private val _state = MutableStateFlow(SettingsUiState())
    val state: StateFlow<SettingsUiState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            runCatchingCancellable { TunnelBridge.getSettings() }
                .onSuccess { _state.value = _state.value.copy(settings = it, loaded = true) }
        }
    }

    fun showKillswitchGuide() {
        _state.value = _state.value.copy(killswitchGuideShown = true)
    }

    fun dismissKillswitchGuide() {
        _state.value = _state.value.copy(killswitchGuideShown = false)
    }

    fun dismissError() {
        _state.value = _state.value.copy(error = null)
    }

    fun openVpnSettings(): Intent = Intent(Settings.ACTION_VPN_SETTINGS)

    fun setPreferredTransport(kind: String) = store { it.copy(preferredTransport = kind) }

    fun setMtu(mtu: Int) = store { it.copy(mtu = mtu) }

    fun setCustomDns(value: String) = store {
        it.copy(customDns = value.split(',', ' ', '\t').map(String::trim).filter(String::isNotEmpty))
    }

    fun setAutoConnect(enabled: Boolean) = store { it.copy(autoConnect = enabled) }

    fun setAllowLan(enabled: Boolean) = store { it.copy(allowLan = enabled) }

    /** Goes through the core so the last-method rule is enforced in one place. */
    fun setHubMethod(method: String, enabled: Boolean) {
        viewModelScope.launch {
            runCatchingCancellable { TunnelBridge.setHubMethod(method, enabled) }
                .onSuccess { _state.value = _state.value.copy(settings = it, error = null) }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    /** The core returns what it actually stored, so a rejected value shows the
     *  corrected one rather than a stale optimistic edit. */
    private fun store(transform: (AppSettings) -> AppSettings) {
        viewModelScope.launch {
            val next = transform(_state.value.settings)
            runCatchingCancellable { TunnelBridge.setSettings(next) }
                .onSuccess { _state.value = _state.value.copy(settings = it, error = null) }
                .onFailure { _state.value = _state.value.copy(error = it.message) }
        }
    }

    fun signOut() {
        viewModelScope.launch {
            PangeaVpnService.disconnect(getApplication())
            runCatchingCancellable { SessionRepository.logout() }
            _state.value = _state.value.copy(signedOut = true)
        }
    }
}
