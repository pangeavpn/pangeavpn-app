package org.pangeavpn.core.data

import android.annotation.SuppressLint
import android.content.Context
import android.content.Intent
import android.net.VpnService
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.CANDIDATE_LIMIT
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.R
import org.pangeavpn.core.Region
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.connection.ConnectProgress
import org.pangeavpn.core.connection.HeroRules
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.pickRandomServer
import org.pangeavpn.core.regionKeyOf
import org.pangeavpn.core.util.runCatchingCancellable

private const val ROUTE_DEBOUNCE_MS = 600L

// The service shows CONNECTING promptly; past this the status is taken to describe the old tunnel.
private const val ACCEPT_TIMEOUT_MS = 3_000L
private const val RELEASE_TIMEOUT_MS = 5_000L

data class ConnectionState(
    // A start is being prepared or handed to the service.
    val busy: Boolean = false,
    // An in-place move is under way.
    val switching: Boolean = false,
    val progress: ConnectProgress? = null,
    val error: String? = null,
    val showServerSheet: Boolean = false,
) {
    val inFlight: Boolean get() = busy || switching
}

private enum class Move { LANDED, UNSEEN, FAILED }

/** What the user asks of the tunnel. Call [init] first; everything else runs on the main thread. */
@SuppressLint("StaticFieldLeak")
object ConnectionRepository {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private lateinit var appContext: Context

    val status: StateFlow<TunnelStatus> = TunnelBridge.status

    private val _state = MutableStateFlow(ConnectionState())
    val state: StateFlow<ConnectionState> = _state.asStateFlow()

    private val _consentRequest = MutableStateFlow<Intent?>(null)

    /** The system's VPN consent screen: launch it, call [consentLaunched], answer with [onConsentResult]. */
    val consentRequest: StateFlow<Intent?> = _consentRequest.asStateFlow()

    // A connect waits for the consent answer; kept apart from the request so a recreated Activity never relaunches it.
    private var consentParked = false

    val canRotate: StateFlow<Boolean> = combine(
        TunnelBridge.status.map { it.state }.distinctUntilChanged(),
        _state,
        SessionRepository.subscription,
        ServersRepository.state,
        SettingsRepository.settings,
    ) { tunnel, own, subscription, servers, settings ->
        rotatable(tunnel, own, subscription?.entitled != false, servers, settings.preferredTransport)
    }.stateIn(scope, SharingStarted.Eagerly, false)

    val lockdownBlocksDisconnect: Boolean
        get() = HeroRules.lockdownBlocks(AppPrefs.value.lockdown, status.value.state)

    // Set by a user stop, so an attempt it cut short is not reported as a failure.
    private var stopRequested = false

    // True until the tunnel is up, so a reconnect or switch passing back through connecting is no new connect.
    private var comingUp = true
    private var progressJob: Job? = null
    private var routeJob: Job? = null

    private val transport: String get() = SettingsRepository.settings.value.preferredTransport

    /** Every Activity and the tile call this before anything else here. */
    fun init(context: Context) {
        appContext = context.applicationContext
        TunnelBridge.init(appContext)
    }

    fun toggle() {
        scope.launch { toggleNow() }
    }

    /** Connects when idle, switches in place when up, only selects while disconnecting. */
    fun activate(region: Region) {
        scope.launch {
            val nodes = region.servers.filter { it.supports(transport) }
            val usable = if (nodes.isEmpty()) region else Region(region.key, nodes)
            if (usable.servers.isNotEmpty()) move(usable.best) { ServersRepository.select(usable) }
        }
    }

    /** As [activate] for a region, but the node is pinned. */
    fun activate(server: Server) {
        scope.launch { move(server) { ServersRepository.select(server, pinned = true) } }
    }

    /** Lands on the first server that answers and pins it; on failure the old pick comes back. */
    fun rotate() {
        scope.launch { rotateNow() }
    }

    /** Debounced, and it outlives the picker: moving to the server it is on re-prepares the route. */
    fun routeChanged() {
        routeJob?.cancel()
        routeJob = scope.launch {
            delay(ROUTE_DEBOUNCE_MS)
            routeJob = null
            applyRoute()
        }
    }

    fun setMultihop(enabled: Boolean) {
        AppPrefs.update { it.copy(multihopEnabled = enabled) }
        routeChanged()
    }

    /** Null or empty lets the route pick its own entry. */
    fun setMultihopEntry(serverId: String?) {
        AppPrefs.update { it.copy(multihopEntryId = serverId?.takeIf { id -> id.isNotEmpty() }) }
        routeChanged()
    }

    fun setServerSheetShown(shown: Boolean) {
        _state.update { it.copy(showServerSheet = shown) }
    }

    /** Whether a connect would have to ask for VPN consent, which only an Activity can do. */
    fun needsConsent(): Boolean = VpnService.prepare(appContext) != null

    fun consentLaunched() {
        _consentRequest.value = null
    }

    /** A granted consent resumes the connect that asked for it; a refused one drops it. */
    fun onConsentResult(granted: Boolean) {
        val parked = consentParked
        consentParked = false
        _consentRequest.value = null
        if (granted && parked) scope.launch { startIdle(interactive = true) }
    }

    /** For callers with no Activity: never parks on consent. False when the app has to be opened. */
    suspend fun connectUnattended(): Boolean =
        withContext(Dispatchers.Main.immediate) { startIdle(interactive = false) }

    // A tap while stopping is ignored, so it cannot queue a start behind the stop.
    private suspend fun toggleNow() {
        if (_state.value.busy) return
        when (status.value.state) {
            ConnState.CONNECTED, ConnState.CONNECTING -> {
                if (AppPrefs.value.lockdown) return
                stopRequested = true
                PangeaVpnService.disconnect(appContext)
            }
            ConnState.DISCONNECTING -> Unit
            ConnState.DISCONNECTED, ConnState.ERROR -> startIdle(interactive = true)
        }
    }

    // False when the connect needs the app first: consent, a server to pick, a subscription.
    private suspend fun startIdle(interactive: Boolean): Boolean {
        val state = status.value.state
        if (_state.value.inFlight || (state != ConnState.DISCONNECTED && state != ConnState.ERROR)) return true
        return whileBusy { connect(releasingFirst = state == ConnState.ERROR, interactive = interactive) }
    }

    // A running tunnel moves in place; an idle one starts on the new pick.
    private suspend fun move(target: Server, select: suspend () -> Unit) {
        if (_state.value.inFlight) return
        setServerSheetShown(false)
        val now = status.value
        when (now.state) {
            ConnState.CONNECTED, ConnState.CONNECTING ->
                if (target.id == now.serverId) select() else switchInPlace(target, select)
            ConnState.DISCONNECTED, ConnState.ERROR -> whileBusy {
                select()
                connect(releasingFirst = now.state == ConnState.ERROR)
            }
            ConnState.DISCONNECTING -> select()
        }
    }

    private suspend fun connect(releasingFirst: Boolean, interactive: Boolean = true): Boolean {
        if (!SessionRepository.entitled) {
            setError(text(R.string.error_subscription_expired))
            return false
        }
        setError(null)
        stopRequested = false
        // The service reads Allow LAN and the transport from the core, so they must be the real ones.
        if (runCatchingCancellable { SettingsRepository.ensureLoaded() }.isFailure) {
            setError(text(R.string.error_connect_failed))
            return false
        }
        // With no Activity the list may not have loaded yet; the last server still names a target.
        val targetId = connectTarget()?.id
            ?: SettingsRepository.settings.value.lastServerId.takeIf { !interactive && it.isNotEmpty() }
        if (targetId == null) {
            setServerSheetShown(true)
            return false
        }
        val consent = VpnService.prepare(appContext)
        if (consent != null) {
            if (interactive && !consentParked) {
                consentParked = true
                _consentRequest.value = consent
            }
            return false
        }
        // A failed tunnel can still hold its routes and sink this start; Lockdown wants them kept.
        if (releasingFirst && !AppPrefs.value.lockdown) release()
        start(targetId, ServersRepository.candidates(transport))
        return true
    }

    // The pick if it carries the transport, else its region's best node that does, else a light node.
    private suspend fun connectTarget(): Server? {
        val servers = ServersRepository.state.value
        val visible = servers.regionsFor(transport)
        val current = servers.selected
        if (current != null) {
            if (current.supports(transport)) return current
            val region = visible.firstOrNull { it.key == regionKeyOf(current) }
            if (region != null) {
                ServersRepository.select(region)
                return ServersRepository.state.value.selected
            }
        }
        val pick = pickRandomServer(visible.flatMap { it.servers }) ?: return null
        ServersRepository.select(pick, pinned = false)
        return pick
    }

    private suspend fun release() {
        PangeaVpnService.disconnect(appContext)
        withTimeoutOrNull(RELEASE_TIMEOUT_MS) { status.first { it.state == ConnState.DISCONNECTED } }
    }

    // Over once the service has taken the start; how it ends shows in the status. False if it never showed.
    private suspend fun start(serverId: String, candidates: List<String>): Boolean {
        val from = status.value.state
        PangeaVpnService.connect(appContext, serverId, candidates)
        return withTimeoutOrNull(ACCEPT_TIMEOUT_MS) { status.first { it.state != from } } != null
    }

    private suspend fun switchInPlace(target: Server, select: suspend () -> Unit) {
        whileSwitching {
            stopRequested = false
            val previous = ServersRepository.state.value.selected
            val previousPin = ServersRepository.state.value.pinnedServerId
            val leaving = status.value.serverId
            select()
            when (moveTunnel(target.id, candidates(leading = target.id))) {
                Move.LANDED, Move.FAILED -> settleSelection(previous, previousPin)
                Move.UNSEEN -> adoptLanded(ignoring = leaving)
            }
        }
    }

    private suspend fun rotateNow() {
        val servers = ServersRepository.state.value
        if (!rotatable(status.value.state, _state.value, SessionRepository.entitled, servers, transport)) return
        val leaving = servers.selected ?: return
        val rotation = servers.rotation(transport)
        val first = rotation.plan.firstOrNull()?.let { servers.server(it) } ?: return
        val previousPin = servers.pinnedServerId
        whileSwitching {
            stopRequested = false
            ServersRepository.select(first, pinned = false)
            val move = moveTunnel(first.id, rotation.plan.take(CANDIDATE_LIMIT))
            val cancelled = stopRequested
            val reported = status.value.serverId
            val landedId = when {
                cancelled || move == Move.FAILED -> null
                reported in rotation.plan -> reported
                move == Move.UNSEEN -> first.id
                else -> null
            }
            if (!cancelled) ServersRepository.recordRotation(rotation, leaving.id, landedId)
            val landed = landedId?.let { ServersRepository.state.value.server(it) }
            if (landed != null) {
                ServersRepository.adopt(landed, pinnedId = landed.id)
            } else {
                ServersRepository.adopt(leaving, previousPin)
            }
        }
    }

    private suspend fun applyRoute() {
        val now = status.value
        if (now.state != ConnState.CONNECTED || _state.value.inFlight) return
        val current = now.serverId.ifEmpty { ServersRepository.state.value.selected?.id.orEmpty() }
        if (current.isEmpty()) return
        whileSwitching {
            stopRequested = false
            val move = moveTunnel(current, candidates(leading = current))
            if (move == Move.LANDED) settleSelection(previous = null, previousPin = null)
        }
    }

    // Waits for the move to show, then to end. UNSEEN: the status never showed it, so it proves nothing.
    private suspend fun moveTunnel(serverId: String, candidates: List<String>): Move {
        setError(null)
        val outage = status.value.reconnecting
        if (PangeaVpnService.switchTo(serverId, candidates)) {
            // In an outage the service takes the new target once the network is back; nothing to watch.
            if (outage) return Move.UNSEEN
            withTimeoutOrNull(ACCEPT_TIMEOUT_MS) { status.first { it.state != ConnState.CONNECTED } }
                ?: return Move.UNSEEN
        } else {
            // No tunnel to move, so this is a plain start, unless the user stopped meanwhile.
            if (stopRequested) return Move.FAILED
            if (!start(serverId, candidates)) return Move.UNSEEN
        }
        // Over when the tunnel is up again, down, or back in the service's own retry loop.
        val end = status.first { it.state != ConnState.CONNECTING || it.reconnecting }
        if (end.state == ConnState.CONNECTED) return Move.LANDED
        if (end.state == ConnState.ERROR || end.state == ConnState.CONNECTING) {
            report(end.detail.ifEmpty { text(R.string.error_connect_failed) })
        }
        return Move.FAILED
    }

    // Up on another server than the pick, so the pick goes back to it and the screen tells the truth.
    private suspend fun settleSelection(previous: Server?, previousPin: String?) {
        val now = status.value
        val selectedId = ServersRepository.state.value.selected?.id
        if (now.state != ConnState.CONNECTED || now.serverId.isEmpty() || now.serverId == selectedId) return
        if (previous != null && previous.id == now.serverId) {
            ServersRepository.adopt(previous, previousPin)
        } else {
            ServersRepository.follow(now.serverId)
        }
    }

    // The server being left is not a landing; the status may still name it until the move shows.
    private suspend fun adoptLanded(ignoring: String) {
        val now = status.value
        val selectedId = ServersRepository.state.value.selected?.id
        if (now.state != ConnState.CONNECTED || now.serverId.isEmpty()) return
        if (now.serverId == ignoring || now.serverId == selectedId) return
        ServersRepository.follow(now.serverId)
    }

    private fun candidates(leading: String): List<String> =
        (listOf(leading) + ServersRepository.candidates(transport).filter { it != leading }).take(CANDIDATE_LIMIT)

    private fun rotatable(
        tunnel: ConnState,
        own: ConnectionState,
        entitled: Boolean,
        servers: ServersState,
        transport: String,
    ): Boolean = tunnel == ConnState.CONNECTED && !own.inFlight && entitled &&
        servers.rotation(transport).plan.isNotEmpty()

    private fun entered(now: TunnelStatus) {
        // A tunnel that got going makes the last connect error stale.
        if (HeroRules.isUp(now.state)) setError(null)
        when (now.state) {
            ConnState.CONNECTED -> {
                // Each connect checks the subscription once; the service's own reconnects never do.
                if (comingUp) scope.launch { SessionRepository.refreshSubscription() }
                comingUp = false
            }
            ConnState.DISCONNECTED, ConnState.ERROR -> comingUp = true
            ConnState.CONNECTING, ConnState.DISCONNECTING -> Unit
        }
        trackProgress(now.state)
    }

    // Adopts where a running tunnel really is, such as after a boot start or a fallback.
    private fun followTunnel(previous: TunnelStatus, now: TunnelStatus) {
        val id = now.serverId
        val moved = previous.state != ConnState.CONNECTED || previous.serverId != id
        if (now.state != ConnState.CONNECTED || !moved || _state.value.inFlight || id.isEmpty()) return
        if (id == ServersRepository.state.value.selected?.id) return
        scope.launch { ServersRepository.follow(id) }
    }

    private fun trackProgress(state: ConnState) {
        progressJob?.cancel()
        progressJob = null
        _state.update { it.copy(progress = null) }
        if (state != ConnState.CONNECTING) return
        progressJob = scope.launch {
            for (step in ConnectProgress.entries) {
                delay(step.waitMs)
                _state.update { it.copy(progress = step) }
            }
        }
    }

    private fun report(message: String) {
        if (!stopRequested) setError(message)
    }

    private fun setError(message: String?) {
        _state.update { it.copy(error = message) }
    }

    private fun text(id: Int): String = appContext.getString(id)

    private suspend fun <T> whileBusy(block: suspend () -> T): T {
        _state.update { it.copy(busy = true) }
        try {
            return block()
        } finally {
            _state.update { it.copy(busy = false) }
        }
    }

    private suspend fun whileSwitching(block: suspend () -> Unit) {
        _state.update { it.copy(switching = true) }
        try {
            block()
        } finally {
            _state.update { it.copy(switching = false) }
        }
    }

    // Last, so the first collect (which Main.immediate may run right here) finds the state above.
    init {
        scope.launch {
            var previous = TunnelStatus()
            status.collect { now ->
                if (now.state != previous.state) entered(now)
                followTunnel(previous, now)
                previous = now
            }
        }
        scope.launch {
            // A picker or consent left open by a sign-out or wipe would reopen over the next account.
            SessionRepository.session.map { it != null }.distinctUntilChanged().collect { signedIn ->
                if (!signedIn) {
                    consentParked = false
                    _consentRequest.value = null
                    setServerSheetShown(false)
                }
            }
        }
    }
}
