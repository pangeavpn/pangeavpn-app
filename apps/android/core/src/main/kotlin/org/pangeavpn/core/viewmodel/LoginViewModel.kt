package org.pangeavpn.core.viewmodel

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.util.runCatchingCancellable

data class LoginUiState(
    val loading: Boolean = false,
    val error: String? = null,
    val needsConsent: Boolean = false,
    val loggedIn: Boolean = false,
)

class LoginViewModel(app: Application) : AndroidViewModel(app) {
    private val _state = MutableStateFlow(LoginUiState())
    val state: StateFlow<LoginUiState> = _state.asStateFlow()

    init {
        TunnelBridge.init(app)
        // loggedIn is authoritative from the session repo, so sign-out flips it too.
        viewModelScope.launch {
            SessionRepository.session.collect { s ->
                _state.value = _state.value.copy(loggedIn = s != null)
            }
        }
        restore()
    }

    fun restore() {
        viewModelScope.launch {
            _state.value = _state.value.copy(loading = true, error = null)
            runCatchingCancellable { SessionRepository.restore() }
            _state.value = _state.value.copy(loading = false)
        }
    }

    fun login(token: String) {
        if (token.isBlank()) {
            _state.value = _state.value.copy(error = "Enter your access token")
            return
        }
        viewModelScope.launch {
            _state.value = _state.value.copy(loading = true, error = null)
            runCatchingCancellable { SessionRepository.login(token) }
                .onFailure { e ->
                    _state.value = _state.value.copy(loading = false, error = e.message ?: "Login failed")
                }
                .onSuccess { _state.value = _state.value.copy(loading = false) }
        }
    }

    fun clearError() {
        _state.value = _state.value.copy(error = null)
    }
}
