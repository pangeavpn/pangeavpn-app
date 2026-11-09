package org.pangeavpn.core.tunnel

import android.os.ParcelFileDescriptor
import android.os.SystemClock
import android.util.Log
import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.NetworkMonitor
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.PathUpdate
import org.pangeavpn.core.SplitTunnelStore
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.TunnelConfig
import org.pangeavpn.core.openAppIntent
import org.pangeavpn.core.startCandidates
import org.pangeavpn.core.util.runCatchingCancellable

private const val TAG = "PangeaTunnel"
private const val AUTO_TRANSPORT = "auto"

/** What the app asked for; a system start (Always-on, boot, sticky restart) has none. */
internal data class StartRequest(val serverId: String, val candidates: List<String>)

internal sealed interface MultihopChoice {
    data object Direct : MultihopChoice
    data class Via(val entry: String) : MultihopChoice

    companion object {
        // null is single hop; "" or the exit itself means the lightest entry the core offers.
        suspend fun choose(preference: String?, exit: String, entries: suspend () -> List<String>): MultihopChoice {
            if (preference == null) return Direct
            if (preference.isNotEmpty() && preference != exit) return Via(preference)
            return entries().firstOrNull()?.let { Via(it) } ?: Direct
        }
    }
}

// A newer operation took over; whatever the old one was doing is dropped without a word.
private class Superseded : Exception()

// iOS's TunnelLifecycle, on the main thread. The service keeps its own fd on the TUN and Go only
// ever gets a dup, so no Go stop or failed start can take the interface down.
internal class TunnelLifecycle(private val service: PangeaVpnService) {
    enum class Phase { IDLE, STARTING, RUNNING, RECONNECTING, WAITING, STOPPED }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private val alerts = ConnectionAlerts(service, scope)
    private val startGate = Mutex()

    @Volatile var phase = Phase.IDLE
        private set

    /** Between a start and its stop, so a switch has something to move. */
    val active: Boolean get() = phase in ACTIVE

    private var policy = ReconnectPolicy()
    private var liveness = LivenessPolicy()
    private var targets = CandidateList.of(emptyList())
    private var request: StartRequest? = null
    private var allowLan = false
    private var systemStart = false
    private var sessionReady = false
    private var blockingDone = false
    private var preparedExit: String? = null
    private var prepared: TunnelConfig? = null
    private var landed: TunnelConfig? = null
    private var tun: ParcelFileDescriptor? = null
    private var tunSettings: TunnelSettings? = null
    private var lastStop: Job? = null
    private var cycle = 0
    private var session = 0
    private var pendingStart: Int? = null
    private var attemptTimer: Job? = null
    private var recheckTimer: Job? = null
    private var livenessTimer: Job? = null
    private var wakeTimers: List<Job> = emptyList()
    private var watchJob: Job? = null
    private var latest: PathUpdate? = null
    private var startBaseline: String? = null
    private var everConnected = false
    private var outage = false
    private var switching = false
    private var note: TunnelFailure? = null
    private var connectedSince = 0L
    private var tickUptime = 0.0
    private var tickWall = 0.0

    // The blocking TUN goes up before anything reaches the hub, so nothing leaks while it is reached.
    fun start(request: StartRequest?) {
        resetSession()
        phase = Phase.STARTING
        this.request = request
        systemStart = request == null
        pendingStart = cycle
        watchPath(false)
        watchPath(true)
        publish()
        val started = session
        scope.launch { applyBlocking(started) }
    }

    /** A user stop: Disconnect, the notification, a sign-out or a revoke. */
    fun stop() {
        val announce = phase != Phase.STOPPED
        phase = Phase.STOPPED
        cycle++
        cancelTimers()
        watchPath(false)
        note = null
        pendingStart = null
        everConnected = false
        outage = false
        switching = false
        if (announce) alerts.handle(TunnelEvent.Stopped)
        AppPrefs.update { it.copy(tunnelWanted = false) }
        TunnelBridge.show(TunnelView(ConnState.DISCONNECTING))
        teardown(TunnelView(ConnState.DISCONNECTED))
    }

    /** The service is going away without a user stop; nothing of the tunnel may outlive it. */
    fun release() {
        if (active) {
            phase = Phase.STOPPED
            cycle++
            cancelTimers()
            watchPath(false)
            pendingStart = null
            alerts.handle(TunnelEvent.Stopped)
            TunnelBridge.show(TunnelView(ConnState.DISCONNECTED))
        }
        tun?.closeQuietly()
        tun = null
        tunSettings = null
        stopGo()
    }

    // A switch never leaves the session: it stays up or reconnecting throughout.
    fun switchServer(serverId: String, candidates: List<String>) {
        if (serverId.isEmpty()) return
        when (phase) {
            Phase.IDLE, Phase.STOPPED -> Log.i(TAG, "switch ignored: the tunnel is not running")
            Phase.STARTING -> supersedeStart(serverId, candidates)
            Phase.RUNNING, Phase.RECONNECTING, Phase.WAITING -> beginSwitch(serverId, candidates)
        }
    }

    private suspend fun applyBlocking(started: Int) {
        val lan = runCatchingCancellable { TunnelBridge.getSettings().allowLan }.getOrDefault(false)
        if (started != session || phase != Phase.STARTING) return
        allowLan = lan
        val error = establishTun(TunnelSettingsBuilder.blocking(allowLan, excludedRanges(), excludedApps()))
        val pending = pendingStart ?: return
        if (error != null) return finishStart(error, pending)
        blockingDone = true
        runStartSequence(pending)
    }

    private fun runStartSequence(started: Int) {
        scope.launch {
            val error = runCatchingCancellable { ensureSession(started) }.exceptionOrNull()
            if (error != null) return@launch finishStart(error, started)
            tryCandidates(started)
        }
    }

    private suspend fun tryCandidates(started: Int) {
        var index = 0
        while (true) {
            targets = targets.at(index)
            val error = establish(started, switching = false)
            if (started != cycle || pendingStart != started) return
            if (error == null) return finishStart(null, started)
            val next = index + 1
            if (next >= targets.ids.size || !worthNextCandidate(error)) return finishStart(error, started)
            logFailure("start on candidate ${index + 1}", error)
            index = next
        }
    }

    private fun finishStart(error: Throwable?, started: Int) {
        if (pendingStart != started) return
        pendingStart = null
        if (error != null && !continuesBlocked(error)) return failStart(error)
        startBaseline?.let { policy.seed(it) }
        if (error != null) {
            Log.i(TAG, "start stays up with traffic blocked and keeps retrying")
            phase = Phase.RECONNECTING
            retry(1, error)
        } else {
            Log.i(TAG, "tunnel started")
            cameUp()
        }
        latest?.let { handle(it) }
    }

    // As iOS arms on-demand only once a connect worked, or from the start under Lockdown.
    private fun failStart(error: Throwable) {
        phase = Phase.IDLE
        cancelTimers()
        watchPath(false)
        logFailure("start", error)
        val failure = TunnelFailure.of(error)
        if (!AppPrefs.value.lockdown) AppPrefs.update { it.copy(tunnelWanted = false) }
        teardown(TunnelView(ConnState.ERROR, service.getString(failure.messageRes), failure))
    }

    // Go goes first, then the TUN, so the end state shows only once traffic is back on the network.
    private fun teardown(end: TunnelView) {
        val ending = session
        val endPhase = phase
        val held = tun
        tun = null
        tunSettings = null
        scope.launch {
            stopGo().join()
            held?.closeQuietly()
            if (session != ending || phase != endPhase) return@launch
            TunnelBridge.show(end)
            service.finished()
        }
    }

    // Shared by a start, a reconnect and a switch that succeeded.
    private fun cameUp() {
        phase = Phase.RUNNING
        note = null
        switching = false
        landed = prepared
        if (connectedSince == 0L) connectedSince = System.currentTimeMillis()
        liveness = liveness.armed(uptime())
        scheduleLivenessTick()
        announceUp()
        publish()
    }

    private fun announceUp() {
        if (!everConnected) {
            alerts.handle(TunnelEvent.Connected)
        } else if (outage) {
            alerts.handle(TunnelEvent.Restored)
        }
        everConnected = true
        outage = false
    }

    // Every terminal failure is told, so an alert the open app held back can show once it closes.
    private fun announceTrouble(kind: RetryKind) {
        when (kind) {
            is RetryKind.Terminal -> {
                outage = true
                alerts.handle(TunnelEvent.Blocked(kind.failure))
            }
            RetryKind.Transient -> {
                if (outage) return
                outage = true
                alerts.handle(TunnelEvent.Reconnecting)
            }
        }
    }

    private fun pathChanged(update: PathUpdate) {
        latest = update
        when (phase) {
            Phase.STARTING ->
                if (startBaseline == null && update.usable && update.key.isNotEmpty()) startBaseline = update.key
            Phase.RUNNING, Phase.RECONNECTING, Phase.WAITING -> handle(update)
            Phase.IDLE, Phase.STOPPED -> Unit
        }
    }

    private fun handle(update: PathUpdate) {
        when (val decision = policy.pathChanged(update.key, update.usable, uptime())) {
            PathDecision.Ignore -> Unit
            is PathDecision.Recheck -> {
                recheckTimer?.cancel()
                recheckTimer = after(decision.after) { recheck() }
            }
            PathDecision.WaitForNetwork -> enterWaiting()
            is PathDecision.Reconnect -> {
                Log.i(TAG, "network changed, reconnecting in ${decision.after}s")
                beginRecovery(decision.after)
            }
        }
    }

    private fun recheck() {
        recheckTimer = null
        val update = latest ?: return
        if (phase == Phase.RUNNING || phase == Phase.RECONNECTING || phase == Phase.WAITING) handle(update)
    }

    private fun beginRecovery(delaySeconds: Double) {
        announceTrouble(RetryKind.Transient)
        phase = Phase.RECONNECTING
        cycle++
        cancelTimers()
        switching = false
        scheduleAttempt(1, delaySeconds)
        publish()
    }

    // Go stays stopped and the TUN stays up, so traffic fails closed until the network returns.
    private fun enterWaiting() {
        policy.markWaiting()
        if (phase == Phase.WAITING) return
        phase = Phase.WAITING
        cycle++
        cancelTimers()
        switching = false
        stopGo()
        announceTrouble(RetryKind.Transient)
        publish()
        Log.i(TAG, "network unavailable, waiting")
    }

    // Every third failure in a row moves on to the next candidate, wrapping around.
    private fun retry(afterFailedAttempt: Int, error: Throwable) {
        logFailure("attempt $afterFailedAttempt", error)
        val failure = TunnelFailure.of(error)
        note = failure
        switching = false
        if (latest?.usable == false) {
            enterWaiting()
            publish()
            return
        }
        val missing = failure == TunnelFailure.NO_SERVER && targets.ids.size > 1
        val kind = if (missing) RetryKind.Transient else ReconnectPolicy.classify(failure)
        if (kind is RetryKind.Terminal) sessionReady = false
        announceTrouble(kind)
        if (missing || policy.rotates(afterFailedAttempt)) rotateTarget()
        scheduleAttempt(afterFailedAttempt + 1, policy.delay(kind, afterFailedAttempt))
        publish()
    }

    private fun rotateTarget() {
        val next = targets.advanced()
        if (next == targets) return
        targets = next
        Log.i(TAG, "moving on to the next server")
    }

    private fun scheduleAttempt(attempt: Int, delaySeconds: Double) {
        val current = cycle
        attemptTimer = after(delaySeconds) { runAttempt(attempt, current) }
    }

    // Runs outside the timer's job, so cancelling timers never cuts an attempt off mid-start.
    private fun runAttempt(attempt: Int, current: Int) {
        if (phase != Phase.RECONNECTING || current != cycle) return
        attemptTimer = null
        if (latest?.usable == false) return enterWaiting()
        scope.launch {
            if (attempt == 1) runCatchingCancellable { TunnelBridge.networkChanged() }
            if (current != cycle) return@launch
            if (attempt == 1 && preparedExit == targets.current && restartInPlace(current)) {
                return@launch attemptFinished(attempt, current, null)
            }
            if (current != cycle) return@launch
            stopGo().join()
            attemptFinished(attempt, current, establish(current, switching = false))
        }
    }

    // Go keeps the prepared config across a stop, so this skips the hub and the TUN.
    private suspend fun restartInPlace(current: Int): Boolean {
        if (tun == null) {
            Log.i(TAG, "tunnel interface not found, preparing again")
            return false
        }
        val error = startGo(current, settings = null, forSwitch = false) ?: return true
        if (current == cycle) logFailure("restart in place", error)
        return false
    }

    private fun attemptFinished(attempt: Int, current: Int, error: Throwable?) {
        if (phase != Phase.RECONNECTING || current != cycle) return
        if (error == null) {
            Log.i(TAG, "reconnected on attempt $attempt")
            return cameUp()
        }
        retry(attempt, error)
    }

    private fun retarget(serverId: String, candidates: List<String>) {
        val previous = targets.current.ifEmpty { request?.serverId.orEmpty() }
        targets = CandidateList.forSwitch(serverId, previous, candidates)
    }

    // Keeps the pending start, so it finishes on the new server instead.
    private fun supersedeStart(serverId: String, candidates: List<String>) {
        if (pendingStart == null) return
        retarget(serverId, candidates)
        cycle++
        pendingStart = cycle
        Log.i(TAG, "switch replaces the pending start")
        if (blockingDone) runStartSequence(cycle)
    }

    private fun beginSwitch(serverId: String, candidates: List<String>) {
        retarget(serverId, candidates)
        if (phase == Phase.WAITING) {
            Log.i(TAG, "switch saved for when the network returns")
            return
        }
        phase = Phase.RECONNECTING
        cycle++
        cancelTimers()
        switching = true
        publish()
        Log.i(TAG, "switching server in place")
        val current = cycle
        scope.launch { switchFinished(establish(current, switching = true), current) }
    }

    private fun switchFinished(error: Throwable?, current: Int) {
        if (phase != Phase.RECONNECTING || current != cycle) return
        if (error == null) {
            Log.i(TAG, "switched server in place")
            return cameUp()
        }
        retry(1, error)
    }

    // Android has no wake callback, so a tick that finds the wall clock ran ahead of uptime is the wake.
    private fun wake() {
        Log.i(TAG, "device woke")
        if (phase != Phase.RUNNING) return
        liveness = liveness.armed(uptime(), LivenessPolicy.WAKE_GRACE)
        wakeTimers.forEach { it.cancel() }
        wakeTimers = LivenessPolicy.WAKE_CHECKS.map { seconds -> after(seconds) { checkLiveness("wake") } }
    }

    private fun scheduleLivenessTick() {
        livenessTimer?.cancel()
        tickUptime = uptime()
        tickWall = wallSeconds()
        livenessTimer = after(LivenessPolicy.CHECK_INTERVAL) { livenessTick() }
    }

    private suspend fun livenessTick() {
        livenessTimer = null
        if (phase != Phase.RUNNING) return
        val current = cycle
        val slept = (wallSeconds() - tickWall) - (uptime() - tickUptime)
        if (slept >= LivenessPolicy.CHECK_INTERVAL) wake()
        checkLiveness("tick")
        if (phase != Phase.RUNNING || current != cycle) return
        scheduleLivenessTick()
    }

    // Catches a tunnel that died quietly, as after a sleep on the same network with no path change.
    private suspend fun checkLiveness(reason: String) {
        if (phase != Phase.RUNNING) return
        val current = cycle
        val handshake = runCatchingCancellable { TunnelBridge.refreshState().lastHandshake }.getOrDefault(0L)
        if (phase != Phase.RUNNING || current != cycle) return
        val wallNow = wallSeconds()
        val verdict = liveness.verdict(handshake, wallNow, uptime())
        if (verdict != Liveness.STALE) return
        Log.i(TAG, "no handshake for ${(wallNow - handshake).toLong()}s ($reason), reconnecting")
        beginRecovery(0.0)
    }

    // Restores once per session; an account problem clears it so the next attempt restores again.
    private suspend fun ensureSession(current: Int) {
        if (sessionReady) return
        val servers = TunnelBridge.restoreSession().servers
        val settings = TunnelBridge.getSettings()
        if (current != cycle) throw Superseded()
        allowLan = settings.allowLan
        if (targets.isEmpty) {
            val chosen = request?.serverId?.takeIf { it.isNotEmpty() }
                ?: settings.lastServerId.ifEmpty { throw TunnelStartException(TunnelFailure.NO_SERVER) }
            targets = CandidateList.of(candidatesFor(chosen, settings.preferredTransport, servers))
        }
        sessionReady = true
    }

    // System starts bring no list, so they fall back the way the app's own connect would.
    private fun candidatesFor(chosen: String, transport: String, servers: List<Server>): List<String> {
        val request = request
        if (request == null && transport == AUTO_TRANSPORT) return startCandidates(chosen, servers)
        return (listOf(chosen) + request?.candidates.orEmpty()).filter { it.isNotEmpty() }.distinct()
    }

    // A switch stops Go only after the prepare, since the hub keeps the old peer until then.
    private suspend fun establish(current: Int, switching: Boolean): Throwable? = try {
        ensureSession(current)
        val tunnel = prepareRoute(current)
        val settings = TunnelSettingsBuilder.build(tunnel, allowLan, excludedRanges(), excludedApps())
        startGo(current, settings, switching)
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e
    }

    // Every prepare reads the multihop choice, so a reconnect keeps the route the user picked.
    private suspend fun prepareRoute(current: Int): TunnelConfig {
        val exit = targets.current
        val prefs = AppPrefs.value
        val preference = if (prefs.multihopEnabled) prefs.multihopEntryId.orEmpty() else null
        val tunnel = when (val choice = MultihopChoice.choose(preference, exit) { TunnelBridge.multihopEntries(exit) }) {
            MultihopChoice.Direct -> {
                if (preference != null) Log.i(TAG, "no multihop entry available, connecting directly")
                TunnelBridge.prepare(exit)
            }
            is MultihopChoice.Via -> TunnelBridge.prepareVia(exit, choice.entry)
        }
        if (current != cycle) throw Superseded()
        preparedExit = exit
        prepared = tunnel
        return tunnel
    }

    // One Go start at a time: a start Go has not claimed yet would survive a stop sent before it.
    private suspend fun startGo(current: Int, settings: TunnelSettings?, forSwitch: Boolean): Throwable? {
        staleness(current)?.let { return it }
        // Whatever start still holds the gate is stale; stopping it ends its cascade now.
        if (startGate.isLocked) stopGo().join()
        return startGate.withLock { startLocked(current, settings, forSwitch) }
    }

    // The next TUN is up before Go leaves the old one, so traffic is held throughout.
    private suspend fun startLocked(current: Int, settings: TunnelSettings?, forSwitch: Boolean): Throwable? {
        staleness(current)?.let { return it }
        if (settings != null && settings != tunSettings) establishTun(settings)?.let { return it }
        val held = tun ?: return TunnelStartException(TunnelFailure.NO_TUNNEL_INTERFACE)
        stopGo(forSwitch).join()
        staleness(current)?.let { return it }
        val fd = try {
            held.dup().detachFd()
        } catch (e: IOException) {
            return TunnelStartException(TunnelFailure.NO_TUNNEL_INTERFACE, e)
        }
        val result = runCatchingCancellable { TunnelBridge.start(fd.toLong()) }
        val stale = staleness(current)
        // The stop that voided this start may have reached Go before the start did.
        if (stale != null && result.isSuccess) stopGo().join()
        return stale ?: result.exceptionOrNull()
    }

    private fun staleness(current: Int): Throwable? = when {
        phase == Phase.STOPPED -> TunnelStartException(TunnelFailure.STOPPED)
        current != cycle -> Superseded()
        else -> null
    }

    // A failed establish leaves the interface unknown, so the next one is never skipped.
    private fun establishTun(settings: TunnelSettings): Throwable? {
        tunSettings = null
        val made = try {
            TunnelSettingsBuilder.establish(service, settings, openAppIntent(service))
        } catch (e: Exception) {
            return TunnelStartException(TunnelFailure.NO_TUNNEL_INTERFACE, e)
        } ?: return TunnelStartException(TunnelFailure.VPN_PERMISSION)
        val (next, apps) = made
        tun?.closeQuietly()
        tun = next
        tunSettings = settings.copy(excludedApps = apps)
        policy.settingsApplied(uptime())
        if (apps.size != settings.excludedApps.size) SplitTunnelStore(service).setExcluded(apps)
        service.refreshSystemVpn()
        return null
    }

    // Chained, so a later start never races a stop that is still unwinding.
    private fun stopGo(forSwitch: Boolean = false): Job {
        val previous = lastStop
        return scope.launch {
            previous?.join()
            runCatchingCancellable { if (forSwitch) TunnelBridge.stopForSwitch() else TunnelBridge.stop() }
        }.also { lastStop = it }
    }

    private fun publish() {
        val view = when (phase) {
            Phase.STARTING -> TunnelView(ConnState.CONNECTING)
            Phase.RUNNING -> TunnelView(ConnState.CONNECTED, connectedSince = connectedSince, route = landed)
            Phase.RECONNECTING, Phase.WAITING -> TunnelView(
                state = ConnState.CONNECTING,
                detail = note?.let { service.getString(it.messageRes) }.orEmpty(),
                failure = note,
                reconnecting = !switching,
                connectedSince = connectedSince,
                route = landed,
            )
            Phase.IDLE, Phase.STOPPED -> return
        }
        TunnelBridge.show(view)
    }

    private fun watchPath(enabled: Boolean) {
        if (!enabled) {
            watchJob?.cancel()
            watchJob = null
            return
        }
        if (watchJob != null) return
        watchJob = scope.launch { NetworkMonitor(service).updates().collect { pathChanged(it) } }
    }

    private fun resetSession() {
        pendingStart = null
        cycle++
        session++
        cancelTimers()
        policy = ReconnectPolicy()
        liveness = LivenessPolicy()
        targets = CandidateList.of(emptyList())
        request = null
        allowLan = false
        sessionReady = false
        blockingDone = false
        preparedExit = null
        prepared = null
        landed = null
        tunSettings = null
        latest = null
        startBaseline = null
        everConnected = false
        outage = false
        switching = false
        note = null
        connectedSince = 0L
    }

    private fun cancelTimers() {
        attemptTimer?.cancel()
        attemptTimer = null
        recheckTimer?.cancel()
        recheckTimer = null
        livenessTimer?.cancel()
        livenessTimer = null
        wakeTimers.forEach { it.cancel() }
        wakeTimers = emptyList()
    }

    private fun after(seconds: Double, work: suspend () -> Unit): Job = scope.launch {
        delay((seconds * 1000).toLong())
        work()
    }

    // Lockdown stands in for iOS's on-demand, which turns a failed app start into a blocked system start.
    private fun continuesBlocked(error: Throwable): Boolean =
        (systemStart || AppPrefs.value.lockdown) && blockingDone && !isStopped(error)

    private fun excludedRanges(): List<String> =
        AppPrefs.value.let { if (it.splitTunnelEnabled) it.splitTunnelRanges else emptyList() }

    private fun excludedApps(): Set<String> =
        if (AppPrefs.value.splitTunnelEnabled) SplitTunnelStore(service).excluded() else emptySet()

    // Core text can carry hub and node addresses, so only the mapped kind is logged.
    private fun logFailure(step: String, error: Throwable) {
        if (isStopped(error)) {
            Log.i(TAG, "$step abandoned: stop requested")
        } else {
            Log.w(TAG, "$step failed: ${TunnelFailure.of(error)}")
        }
    }

    private companion object {
        val ACTIVE = setOf(Phase.STARTING, Phase.RUNNING, Phase.RECONNECTING, Phase.WAITING)

        fun uptime(): Double = SystemClock.uptimeMillis() / 1000.0

        fun wallSeconds(): Double = System.currentTimeMillis() / 1000.0

        fun isStopped(error: Throwable): Boolean = (error as? TunnelStartException)?.failure == TunnelFailure.STOPPED

        // A server the hub no longer lists is skipped; an account or permission problem would fail everywhere.
        fun worthNextCandidate(error: Throwable): Boolean {
            if (error is Superseded || isStopped(error)) return false
            val failure = TunnelFailure.of(error)
            if (failure == TunnelFailure.VPN_PERMISSION) return false
            return when (val kind = ReconnectPolicy.classify(failure)) {
                RetryKind.Transient -> true
                is RetryKind.Terminal -> kind.failure == TunnelFailure.NO_SERVER
            }
        }

        fun ParcelFileDescriptor.closeQuietly() {
            runCatching { close() }
        }
    }
}
