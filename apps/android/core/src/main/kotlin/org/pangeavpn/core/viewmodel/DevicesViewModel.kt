package org.pangeavpn.core.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.session.DeviceError
import org.pangeavpn.core.session.DeviceRename
import org.pangeavpn.core.session.LoginErrors
import org.pangeavpn.core.session.RenameError
import org.pangeavpn.core.session.coreErrorText
import org.pangeavpn.core.session.currentFirst
import org.pangeavpn.core.util.runCatchingCancellable

data class DevicesUiState(
    val devices: List<Device> = emptyList(),
    val loading: Boolean = false,
    // A load has finished at least once, whatever it brought back.
    val loaded: Boolean = false,
    // The device being removed or renamed; one operation runs at a time.
    val busyId: String? = null,
    val error: DeviceError? = null,
)

/** The account's devices, for the devices page and the device-limit screen, which has no session yet. */
class DevicesViewModel : ViewModel() {
    private val _state = MutableStateFlow(DevicesUiState())
    val state: StateFlow<DevicesUiState> = _state.asStateFlow()

    // Bumped when the account goes, so an answer still in flight for it is dropped.
    private var generation = 0

    init {
        viewModelScope.launch {
            SessionRepository.session.collect { if (it == null) reset() }
        }
    }

    /** Call when the screen opens, and again for a retry or a pull to refresh. */
    suspend fun load(): Unit = op { reload() }

    /** True once the device is gone and the list is fresh; ignored while another operation runs. */
    suspend fun remove(id: String): Boolean = op {
        if (_state.value.busyId != null) return@op false
        val began = generation
        _state.update { it.copy(busyId = id, error = null) }
        val failure = runCatchingCancellable { TunnelBridge.removeDevice(id) }.exceptionOrNull()
        if (began != generation) return@op false
        if (failure != null) {
            fail(DeviceError.Remove(LoginErrors.general(coreErrorText(failure))))
            return@op false
        }
        reload()
        if (began != generation) return@op false
        _state.update { it.copy(busyId = null) }
        _state.value.error == null
    }

    /** True once the hub stored the name; the list then shows the name it stored. */
    suspend fun rename(id: String, name: String): Boolean = op {
        if (_state.value.busyId != null) return@op false
        val trimmed = DeviceRename.validated(name)
        if (trimmed == null) {
            fail(DeviceError.Rename(RenameError.LENGTH))
            return@op false
        }
        val began = generation
        _state.update { it.copy(busyId = id, error = null) }
        val stored = runCatchingCancellable { TunnelBridge.renameDevice(id, trimmed) }
        if (began != generation) return@op false
        val failure = stored.exceptionOrNull()
        if (failure != null) {
            fail(DeviceError.Rename(DeviceRename.error(coreErrorText(failure))))
            return@op false
        }
        val shown = stored.getOrNull().orEmpty().ifEmpty { trimmed }
        _state.update { state ->
            state.copy(
                busyId = null,
                devices = state.devices.map { if (it.id == id) it.copy(friendlyName = shown) else it },
            )
        }
        true
    }

    // In the model's own scope, so a screen that goes away mid-call never leaves busyId set.
    private suspend fun <T> op(block: suspend () -> T): T = viewModelScope.async { block() }.await()

    private suspend fun reload() {
        val began = generation
        _state.update { it.copy(loading = true, error = null) }
        val listed = runCatchingCancellable { TunnelBridge.listDevices() }
        if (began != generation) return
        val failure = listed.exceptionOrNull()
        _state.update {
            if (failure == null) {
                it.copy(devices = currentFirst(listed.getOrNull().orEmpty()), loading = false, loaded = true)
            } else {
                val error = DeviceError.Load(LoginErrors.general(coreErrorText(failure)))
                it.copy(loading = false, loaded = true, error = error)
            }
        }
    }

    private fun fail(error: DeviceError) {
        _state.update { it.copy(busyId = null, error = error) }
    }

    private fun reset() {
        generation++
        _state.value = DevicesUiState()
    }
}
