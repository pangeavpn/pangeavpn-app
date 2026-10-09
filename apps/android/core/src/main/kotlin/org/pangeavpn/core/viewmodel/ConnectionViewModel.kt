package org.pangeavpn.core.viewmodel

import android.app.Application
import android.content.Intent
import android.net.VpnService
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.util.runCatchingCancellable

data class ConnectionUiState(
    val servers: List<Server> = emptyList(),
    val selectedServerId: String? = null,
    val subscription: Subscription? = null,
    val loading: Boolean = false,
    val error: String? = null,
)

class ConnectionViewModel(app: Application) : AndroidViewModel(app) {
    init {
        TunnelBridge.init(app)
    }

    val status: StateFlow<TunnelStatus> = TunnelBridge.status

    private val _uiState = MutableStateFlow(ConnectionUiState())
    val uiState: StateFlow<ConnectionUiState> = _uiState.asStateFlow()

    private val _consentIntent = MutableStateFlow<Intent?>(null)
    /** Activity must launch this for a result, then call [connect] again with [pendingServerId]. */
    val consentIntent: StateFlow<Intent?> = _consentIntent.asStateFlow()

    var pendingServerId: String? = null
        private set

    fun selectServer(serverId: String) {
        _uiState.value = _uiState.value.copy(selectedServerId = serverId)
    }

    fun refreshServers() {
        viewModelScope.launch {
            _uiState.value = _uiState.value.copy(loading = true)
            runCatchingCancellable { SessionRepository.refreshServers() }
                .onSuccess { _uiState.value = _uiState.value.copy(loading = false, servers = it, error = null) }
                .onFailure { e -> _uiState.value = _uiState.value.copy(loading = false, error = e.message) }
        }
    }

    fun loadSubscription() {
        viewModelScope.launch {
            runCatchingCancellable { SessionRepository.subscription() }
                .onSuccess { _uiState.value = _uiState.value.copy(subscription = it) }
        }
    }

    fun connect(serverId: String) {
        val context = getApplication<Application>()
        val consent = VpnService.prepare(context)
        if (consent != null) {
            pendingServerId = serverId
            _consentIntent.value = consent
            return
        }
        pendingServerId = null
        PangeaVpnService.connect(context, serverId)
    }

    fun consentIntentConsumed() {
        _consentIntent.value = null
    }

    fun disconnect() {
        PangeaVpnService.disconnect(getApplication())
    }
}
