package org.pangeavpn.core.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.model.AppSettings
import org.pangeavpn.core.model.HubStatus
import org.pangeavpn.core.model.HubTestResult
import org.pangeavpn.core.session.coreErrorText
import org.pangeavpn.core.settings.HubMethodRules
import org.pangeavpn.core.util.runCatchingCancellable

// Go's refusal to switch off the last hub method, in mobile.go's SetHubMethod.
private const val LAST_METHOD_MARKER = "at least one hub connection method must stay enabled"

/** Why the last settings read or write failed, for the note a page shows. */
enum class SettingsError { LAST_HUB_METHOD, FAILED }

/** What became of a hub-method tap. IGNORED: that method's earlier tap is still saving. */
enum class HubMethodChange { SAVED, IGNORED, LAST_METHOD, FAILED }

/** The Go core's settings, shared by every screen and the service: the Android SettingsStore. */
object SettingsRepository {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    // Config edits read then write, so they run one at a time or an edit could be lost.
    private val gate = Mutex()

    private val _settings = MutableStateFlow(AppSettings())
    val settings: StateFlow<AppSettings> = _settings.asStateFlow()

    private val _loaded = MutableStateFlow(false)
    val loaded: StateFlow<Boolean> = _loaded.asStateFlow()

    private val _error = MutableStateFlow<SettingsError?>(null)
    /** Set by a failed [load], [save] or [setHubMethod], cleared by the next success; a page calls [clearError] when left. */
    val error: StateFlow<SettingsError?> = _error.asStateFlow()

    private val _pendingHubMethods = MutableStateFlow<Map<String, Boolean>>(emptyMap())
    /** Hub-method taps still saving, by method; they already count as on or off, see [HubMethodRules]. */
    val pendingHubMethods: StateFlow<Map<String, Boolean>> = _pendingHubMethods.asStateFlow()

    private val _testingHubMethod = MutableStateFlow<String?>(null)
    /** The hub method being probed; one test runs at a time. */
    val testingHubMethod: StateFlow<String?> = _testingHubMethod.asStateFlow()

    private val _hubStatus = MutableStateFlow(HubStatus())
    /** The method this process last reached the hub by, as of the last [refreshHubStatus]. */
    val hubStatus: StateFlow<HubStatus> = _hubStatus.asStateFlow()

    init {
        scope.launch {
            SessionRepository.session
                .map { it != null }
                .distinctUntilChanged()
                .collect { signedIn -> if (signedIn) runCatchingCancellable { ensureLoaded() } }
        }
    }

    /** Loads from the core once; later calls return at once. Throws if the core cannot be read. */
    suspend fun ensureLoaded() {
        if (_loaded.value) return
        // Queued with the edits, so a slow load cannot land after an edit and undo it.
        gate.withLock {
            if (!_loaded.value) adopt(TunnelBridge.getSettings())
        }
    }

    /** Serialized read-modify-write through the core; returns what the core actually stored. */
    suspend fun edit(transform: (AppSettings) -> AppSettings): AppSettings =
        store { TunnelBridge.setSettings(transform(TunnelBridge.getSettings())) }

    /** [edit] for a settings page: false when the save failed, with the reason left in [error]. */
    suspend fun save(transform: (AppSettings) -> AppSettings): Boolean = recorded { edit(transform) }

    /** [ensureLoaded] for a settings page: false when the core could not be read. */
    suspend fun load(): Boolean = recorded { ensureLoaded() }

    /** Flips one hub method. The core refuses to switch off the last one, and so does this, counting taps still saving. */
    suspend fun setHubMethod(method: String, enabled: Boolean): HubMethodChange {
        val pending = _pendingHubMethods.value
        if (method in pending) return HubMethodChange.IGNORED
        if (!enabled && HubMethodRules.isLocked(method, _settings.value.hubMethods, pending)) {
            return HubMethodChange.LAST_METHOD
        }
        _pendingHubMethods.update { it + (method to enabled) }
        try {
            if (recorded { store { TunnelBridge.setHubMethod(method, enabled) } }) return HubMethodChange.SAVED
            return if (_error.value == SettingsError.LAST_HUB_METHOD) HubMethodChange.LAST_METHOD else HubMethodChange.FAILED
        } finally {
            _pendingHubMethods.update { it - method }
        }
    }

    /** Probes one method; null while another test runs. A probe that could not run at all reads as failed. */
    suspend fun testHubMethod(method: String): HubTestResult? {
        if (!_testingHubMethod.compareAndSet(null, method)) return null
        val result = try {
            runCatchingCancellable { TunnelBridge.testHubMethod(method) }
                .getOrElse { HubTestResult(method = method, detail = coreErrorText(it)) }
        } finally {
            _testingHubMethod.value = null
        }
        refreshHubStatus()
        return result
    }

    suspend fun refreshHubStatus(): HubStatus = TunnelBridge.hubStatus().also { _hubStatus.value = it }

    fun clearError() {
        _error.value = null
    }

    /** Wipe: back to defaults in memory, so nothing on screen outlives the erased values. */
    fun reset() {
        _settings.value = AppSettings()
        _loaded.value = false
        _error.value = null
        _hubStatus.value = HubStatus()
    }

    // The reply is the core's whole stored config, so it also counts as a load.
    private fun adopt(stored: AppSettings) {
        _settings.value = stored
        _loaded.value = true
    }

    // Not cancellable: a page that closes mid-save must still show what the core stored.
    private suspend fun store(write: suspend () -> AppSettings): AppSettings =
        withContext(NonCancellable) { gate.withLock { write().also { adopt(it) } } }

    private suspend fun recorded(action: suspend () -> Unit): Boolean =
        runCatchingCancellable { action() }
            .onSuccess { _error.value = null }
            .onFailure {
                _error.value =
                    if (LAST_METHOD_MARKER in coreErrorText(it)) SettingsError.LAST_HUB_METHOD else SettingsError.FAILED
            }
            .isSuccess
}
