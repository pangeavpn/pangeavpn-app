package org.pangeavpn.core

import android.content.Context
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import org.pangeavpn.core.model.AppSettings
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.model.HubStatus
import org.pangeavpn.core.model.HubTestResult
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.Session
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.model.TunnelConfig
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.tunnel.TunnelFailure
import org.pangeavpn.core.tunnel.TunnelView
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

    private val statusLock = Any()
    private var coreStatus = TunnelStatus()
    private var view: TunnelView? = null

    private val _status = MutableStateFlow(TunnelStatus())
    /** The one status every screen renders: the core's, under the VPN service's phase. */
    val status: StateFlow<TunnelStatus> = _status.asStateFlow()

    private val _failure = MutableStateFlow<TunnelFailure?>(null)
    /** Why the last start or reconnect attempt failed; null once up, on a new start, and after a stop. */
    val failure: StateFlow<TunnelFailure?> = _failure.asStateFlow()

    /** Set by the running [PangeaVpnService] so control-plane dials bypass the TUN. */
    @Volatile var protector: (Long) -> Boolean = { true }

    private val socketProtector = SocketProtector { fd -> protector(fd) }

    // Synchronized so a caller racing the app's warm-up waits for the core rather than using it half set up.
    fun init(context: Context) {
        if (initialized.get()) return
        synchronized(this) {
            if (initialized.get()) return
            val app = context.applicationContext
            AppPrefs.init(app)
            val store = SecretStorePrefs(app)
            Mobile.init(store, socketProtector, this, ConnectivityNetworkKey(app), app.filesDir.absolutePath)
            startPolling()
            initialized.set(true)
        }
    }

    /** "auto" walks the cascade; a transport name pins that one. */
    suspend fun setPreferredTransport(kind: String): Unit = withContext(Dispatchers.IO) {
        Mobile.setPreferredTransport(kind)
    }

    suspend fun preferredTransport(): String = withContext(Dispatchers.IO) {
        Mobile.preferredTransport()
    }

    suspend fun getSettings(): AppSettings = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.getConfig())
    }

    /** Returns what was actually stored, which differs when a value was rejected. */
    suspend fun setSettings(settings: AppSettings): AppSettings = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.setConfig(json.encodeToString(settings)))
    }

    /** Throws when the switch would disable the last way to reach the hub. */
    suspend fun setHubMethod(method: String, enabled: Boolean): AppSettings =
        withContext(Dispatchers.IO) {
            json.decodeFromString(Mobile.setHubMethod(method, enabled))
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
        val core = runCatching { json.decodeFromString<TunnelStatus>(statusJson) }.getOrNull() ?: return
        synchronized(statusLock) {
            coreStatus = core
            publishLocked()
        }
    }

    /** The service's phase decides state and detail; the core fills in the rest. */
    internal fun show(next: TunnelView) {
        synchronized(statusLock) {
            view = next
            publishLocked()
        }
    }

    // Core text is never shown: it can carry dial errors and addresses.
    private fun publishLocked() {
        val shown = view
        _status.value = shown?.applyTo(coreStatus) ?: coreStatus.copy(detail = "")
        _failure.value = shown?.failure
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

    /** Frees this install's device slot; false when the hub did not confirm within [timeoutMs]. Past it the
     *  call carries on here, and the core records the slot so a later launch retries it. */
    suspend fun deregister(timeoutMs: Long): Boolean {
        val call = scope.async { runCatching { Mobile.deregister() } }
        return withTimeoutOrNull(timeoutMs) { call.await() }?.isSuccess == true
    }

    /** Frees a slot an earlier sign-out could not; a no-op when none is pending. */
    suspend fun retryPendingDeregister(): Unit = withContext(Dispatchers.IO) {
        Mobile.retryPendingDeregister()
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

    /** Returns the name the hub stored, which is the trimmed one. */
    suspend fun renameDevice(id: String, name: String): String = withContext(Dispatchers.IO) {
        Mobile.renameDevice(id, name)
    }

    /** The bearer credential, "" unless fully signed in. Never log it. */
    suspend fun accountNumber(): String = withContext(Dispatchers.IO) {
        Mobile.accountNumber()
    }

    /** Deletes the account on the hub, then ends the session here. */
    suspend fun deleteAccount(): Unit = withContext(Dispatchers.IO) {
        Mobile.deleteAccount()
    }

    /** Local only: forgets session, identity, settings, routes and logs. */
    suspend fun wipe(): Unit = withContext(Dispatchers.IO) {
        Mobile.wipe()
    }

    fun setClientInfo(appVersion: String, osVersion: String) {
        Mobile.setClientInfo(appVersion, osVersion)
    }

    suspend fun logs(): String = withContext(Dispatchers.IO) {
        Mobile.logs()
    }

    /** Uploads an anonymous, redacted report and returns its code. */
    suspend fun sendDiagnostics(note: String, extraLogs: String = ""): String =
        withContext(Dispatchers.IO) {
            Mobile.sendDiagnostics(note, extraLogs)
        }

    suspend fun testHubMethod(method: String): HubTestResult = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.testHubMethod(method))
    }

    suspend fun hubStatus(): HubStatus = withContext(Dispatchers.IO) {
        runCatching { json.decodeFromString<HubStatus>(Mobile.hubStatus()) }.getOrDefault(HubStatus())
    }

    /** Forgets the hub route learned on the previous network; never touches a tunnel. */
    suspend fun networkChanged(): Unit = withContext(Dispatchers.IO) {
        Mobile.networkChanged()
    }

    suspend fun prepare(serverId: String): TunnelConfig = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.prepare(serverId))
    }

    /** [entryId] empty is a single hop. */
    suspend fun prepareVia(exitId: String, entryId: String): TunnelConfig = withContext(Dispatchers.IO) {
        json.decodeFromString(Mobile.prepareVia(exitId, entryId))
    }

    /** Ids of the servers that can relay to [exitId], lightest first. */
    suspend fun multihopEntries(exitId: String): List<String> = withContext(Dispatchers.IO) {
        runCatching { json.decodeFromString<List<String>>(Mobile.multihopEntries(exitId)) }
            .getOrDefault(emptyList())
    }

    suspend fun start(fd: Long): Unit = withContext(Dispatchers.IO) {
        Mobile.start(fd)
    }

    suspend fun stop(): Unit = withContext(Dispatchers.IO) {
        Mobile.stop()
    }

    /** Tears down for an immediate start elsewhere, reporting connecting throughout. */
    suspend fun stopForSwitch(): Unit = withContext(Dispatchers.IO) {
        Mobile.stopForSwitch()
    }

    /** Reads the core afresh; a push that moved the core on meanwhile wins over this read. */
    suspend fun refreshState(): TunnelStatus = withContext(Dispatchers.IO) {
        val core = json.decodeFromString<TunnelStatus>(Mobile.state())
        synchronized(statusLock) {
            if (core.state == coreStatus.state) {
                coreStatus = core
                publishLocked()
            }
        }
        core
    }
}
