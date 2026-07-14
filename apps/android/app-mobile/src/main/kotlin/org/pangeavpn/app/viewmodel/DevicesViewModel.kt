package org.pangeavpn.app.viewmodel

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.util.runCatchingCancellable

data class DevicesUiState(
    val loading: Boolean = false,
    val devices: List<Device> = emptyList(),
    val error: String? = null,
)

/** Backs [org.pangeavpn.app.ui.DeviceLimitScreen]: list + remove registered devices, then retry login. */
class DevicesViewModel(app: Application) : AndroidViewModel(app) {
    private val _state = MutableStateFlow(DevicesUiState())
    val state: StateFlow<DevicesUiState> = _state.asStateFlow()

    init {
        TunnelBridge.init(app)
        load()
    }

    fun load() {
        viewModelScope.launch {
            _state.value = _state.value.copy(loading = true, error = null)
            runCatchingCancellable { SessionRepository.devices() }
                .onSuccess { _state.value = _state.value.copy(loading = false, devices = it) }
                .onFailure { e -> _state.value = _state.value.copy(loading = false, error = e.message) }
        }
    }

    fun remove(id: String) {
        viewModelScope.launch {
            runCatchingCancellable { SessionRepository.removeDevice(id) }
                .onSuccess { load() }
                .onFailure { e -> _state.value = _state.value.copy(error = e.message) }
        }
    }
}
