package org.pangeavpn.core

import android.content.Context
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.Session
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.model.TunnelConfig
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.util.runCatchingCancellable
import org.pangeavpn.mobile.Mobile
import org.pangeavpn.mobile.SocketProtector
import org.pangeavpn.mobile.StatusSink

private const val POLL_INTERVAL_MS = 2_000L

/** Kotlin-side wrapper over the gomobile `Mobile` API. Single instance for the process. */
object TunnelBridge : StatusSink {
    private val json = Json { ignoreUnknownKeys = true }
    private val initialized = AtomicBoolean(false)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private val _status = MutableStateFlow(TunnelStatus())
    val status: StateFlow<TunnelStatus> = _status.asStateFlow()

    /** Set by the running [PangeaVpnService] so control-plane dials bypass the TUN. */
    @Volatile var protector: (Long) -> Boolean = { true }

    private val socketProtector = SocketProtector { fd -> protector(fd) }

    fun init(context: Context) {
        if (!initialized.compareAndSet(false, true)) return
        val store = SecretStorePrefs(context.applicationContext)
        Mobile.init(store, socketProtector, this)
        startPolling()
    }

    private fun startPolling() {
        scope.launch {
            while (isActive) {
                delay(POLL_INTERVAL_MS)
                if (_status.value.state == ConnState.CONNECTED) {
                    runCatchingCancellable { refreshState() }
                }
            }
        }
    }

    override fun onStatus(statusJson: String) {
        runCatching { json.decodeFromString<TunnelStatus>(statusJson) }
            .onSuccess { _status.value = it }
    }

    /** Records a client-side failure that never reached the Go status pipeline. */
    fun setError(detail: String) {
        _status.value = _status.value.copy(state = ConnState.ERROR, detail = detail)
    }

    suspend fun login(token: String): Session = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.login(token))
    }

    suspend fun restoreSession(): Session = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.restoreSession())
    }

    suspend fun logout(): Unit = withContext(Dispatchers.IO) {
        Mobile.logout()
    }

    suspend fun listServers(): List<Server> = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.listServers())
    }

    suspend fun getSubscription(): Subscription? = withContext(Dispatchers.IO) {
        val raw = Mobile.getSubscription()
        if (raw == "null") null else json.decodeFromString(raw)
    }

    suspend fun listDevices(): List<Device> = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.listDevices())
    }

    suspend fun removeDevice(id: String): Unit = withContext(Dispatchers.IO) {
        Mobile.removeDevice(id)
    }

    suspend fun prepare(serverId: String): TunnelConfig = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.prepare(serverId))
    }

    suspend fun start(fd: Long): Unit = withContext(Dispatchers.IO) {
        Mobile.start(fd)
    }

    suspend fun stop(): Unit = withContext(Dispatchers.IO) {
        Mobile.stop()
    }

    suspend fun refreshState(): Unit = withContext(Dispatchers.IO) {
        _status.value = json.decodeFromString(Mobile.state())
    }
}
