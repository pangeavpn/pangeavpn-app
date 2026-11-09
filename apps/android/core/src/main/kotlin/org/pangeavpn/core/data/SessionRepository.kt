package org.pangeavpn.core.data

import android.content.Context
import android.os.Build
import android.os.SystemClock
import androidx.core.content.pm.PackageInfoCompat
import java.util.concurrent.atomic.AtomicBoolean
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Session
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.session.DeviceLimitRetry
import org.pangeavpn.core.session.ExpiredVerdict
import org.pangeavpn.core.session.LoginErrorKind
import org.pangeavpn.core.session.LoginErrors
import org.pangeavpn.core.session.coreErrorText
import org.pangeavpn.core.session.deviceLimitRetry
import org.pangeavpn.core.session.expiredVerdict
import org.pangeavpn.core.session.isSignedOutRestore
import org.pangeavpn.core.session.subscriptionDue
import org.pangeavpn.core.tunnel.TunnelFailure
import org.pangeavpn.core.util.normalizeAccountNumber
import org.pangeavpn.core.util.runCatchingCancellable

// A hub search on a dead network can run for minutes; launch waits on it two seconds at most.
private const val LAUNCH_RESTORE_MS = 2_000L
private const val TUNNEL_DOWN_MS = 5_000L

// One hub search plus its request; a slot still held then is retried on a later launch.
private const val DEREGISTER_MS = 25_000L

/** The signed-in account and everything that starts or ends it: the Android SessionStore. */
object SessionRepository {
    private sealed interface RestoreOutcome {
        data class Restored(val session: Session) : RestoreOutcome
        data object SignedOut : RestoreOutcome
        data object Failed : RestoreOutcome
    }

    private class SignInNotSaved : Exception()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val launched = AtomicBoolean(false)
    private lateinit var app: Context

    private val _session = MutableStateFlow<Session?>(null)
    val session: StateFlow<Session?> = _session.asStateFlow()

    private val _subscription = MutableStateFlow<Subscription?>(null)
    val subscription: StateFlow<Subscription?> = _subscription.asStateFlow()

    private val _restored = MutableStateFlow(false)
    /** False while the splash shows: the launch restore has neither answered nor opened on the stored sign-in. */
    val restored: StateFlow<Boolean> = _restored.asStateFlow()

    private val _restoring = MutableStateFlow(false)
    val restoring: StateFlow<Boolean> = _restoring.asStateFlow()

    private val _restoreFailed = MutableStateFlow(false)
    /** A stored sign-in the hub could not confirm; the app offers a retry rather than sign-in. */
    val restoreFailed: StateFlow<Boolean> = _restoreFailed.asStateFlow()

    private val _loggingIn = MutableStateFlow(false)
    val loggingIn: StateFlow<Boolean> = _loggingIn.asStateFlow()

    private val _signingOut = MutableStateFlow(false)
    val signingOut: StateFlow<Boolean> = _signingOut.asStateFlow()

    private val _deletingAccount = MutableStateFlow(false)
    val deletingAccount: StateFlow<Boolean> = _deletingAccount.asStateFlow()

    private val _loginError = MutableStateFlow<LoginErrorKind?>(null)
    val loginError: StateFlow<LoginErrorKind?> = _loginError.asStateFlow()

    private val _showDeviceLimit = MutableStateFlow(false)
    val showDeviceLimit: StateFlow<Boolean> = _showDeviceLimit.asStateFlow()

    private val _forcedSignOut = MutableStateFlow(false)
    /** The hub ended the session under a running app; say so once, then [consumeForcedSignOut]. */
    val forcedSignOut: StateFlow<Boolean> = _forcedSignOut.asStateFlow()

    /** Unknown counts as entitled; only an explicit false blocks. */
    val entitled: Boolean get() = _subscription.value?.entitled != false

    // Memory only, for the device-limit retry; never persisted.
    @Volatile private var pendingToken: String? = null

    // Bumped when the session clears, so a lookup still in flight for the old account is dropped.
    @Volatile private var generation = 0

    @Volatile private var lastSubscriptionCheck: Long? = null
    private var clientInfoSent = false

    /** Call from every entry activity; only the first call restores. */
    fun start(context: Context) {
        if (!launched.compareAndSet(false, true)) return
        app = context.applicationContext
        TunnelBridge.init(app)
        scope.launch {
            TunnelBridge.failure.collect { if (it == TunnelFailure.SIGNED_OUT) followCoreSignOut() }
        }
        scope.launch {
            restore()
            retryPendingDeregister()
        }
    }

    /** Call each time the app comes back to the front. */
    fun onForeground() {
        if (!_restored.value) return
        scope.launch { retryPendingDeregister() }
        if (_session.value == null) return
        val now = SystemClock.elapsedRealtime()
        if (!subscriptionDue(lastSubscriptionCheck, now, entitled)) return
        lastSubscriptionCheck = now
        scope.launch { fetchSubscription() }
    }

    /** "Try Again" on the Restore Failed screen. */
    suspend fun retryRestore(): Unit = op {
        if (_restoring.value) return@op
        restore()
    }

    // A second tap while one login is in flight would register the device twice.
    suspend fun login(token: String): Unit = op {
        if (_loggingIn.value) return@op
        val normalized = normalizeAccountNumber(token)
        if (normalized.isEmpty()) {
            _loginError.value = LoginErrorKind.EmptyAccount
            return@op
        }
        pendingToken = normalized
        attemptLogin(normalized)
    }

    /** "Continue Sign In" on the device-limit screen, with the number typed before it. */
    suspend fun retryLogin(): DeviceLimitRetry = op {
        val token = pendingToken
        if (!_loggingIn.value && token != null) attemptLogin(token)
        deviceLimitRetry(_session.value != null, _showDeviceLimit.value, _loginError.value != null)
    }

    /** Leaves the device-limit screen; no slot is registered yet, so nothing is freed. */
    suspend fun cancelDeviceLimit(): Unit = signOut()

    fun clearLoginError() {
        _loginError.value = null
    }

    fun consumeForcedSignOut() {
        _forcedSignOut.value = false
    }

    // Runs to the end whoever stops waiting: a screen closing must not leave the account half signed out.
    suspend fun signOut(): Unit = op {
        if (_signingOut.value || _deletingAccount.value) return@op
        _signingOut.value = true
        try {
            val holdsSlot = _session.value != null
            stopTunnel()
            if (holdsSlot) {
                // The deregister request has to leave on the real network, not into a closing tunnel.
                withTimeoutOrNull(TUNNEL_DOWN_MS) {
                    TunnelBridge.status.first { it.state == ConnState.DISCONNECTED || it.state == ConnState.ERROR }
                }
                TunnelBridge.deregister(DEREGISTER_MS)
            }
            TunnelBridge.logout()
            ServersRepository.reset()
            clear()
        } finally {
            _signingOut.value = false
        }
    }

    // The tunnel stays up until the hub confirms, so a refused delete leaves the user as they were.
    suspend fun deleteAccount(): Boolean = op {
        if (_deletingAccount.value || _signingOut.value) return@op false
        _deletingAccount.value = true
        try {
            val deleted = runCatchingCancellable { TunnelBridge.deleteAccount() }.isSuccess
            // A hub answer that the session is dead signs the core out anyway, so the app follows it.
            if (deleted || !holdsAccount()) endSession()
            deleted
        } finally {
            _deletingAccount.value = false
        }
    }

    /** Re-reads the subscription once after a fresh connect; never throws. */
    suspend fun refreshSubscription() {
        op { fetchSubscription() }
    }

    /** "Check again" on the expired panel. */
    suspend fun checkSubscription(): ExpiredVerdict = op {
        val reached = fetchSubscription()
        expiredVerdict(reached, entitled, _subscription.value?.cached == true)
    }

    /** The bearer credential, for the reveal card alone; null when signed out. Never log or save it. */
    suspend fun accountNumber(): String? = TunnelBridge.accountNumber().trim().ifEmpty { null }

    /** Drops the in-memory session after a wipe or a hub-side sign-out; no hub calls. */
    fun clearLocal() = clear()

    private suspend fun <T> op(block: suspend () -> T): T = scope.async { block() }.await()

    // A slow hub never holds the app on its splash: past the deadline a stored sign-in opens the app,
    // and the restore still in flight settles it when it lands.
    private suspend fun restore() {
        _restoring.value = true
        sendClientInfoOnce()
        val began = generation
        val pending = scope.async { restoreOutcome() }
        val prompt = withTimeoutOrNull(LAUNCH_RESTORE_MS) { pending.await() }
        if (prompt != null || !holdsAccount()) {
            settle(prompt ?: pending.await(), began)
            return
        }
        open(Session())
        _restoreFailed.value = false
        finishRestoring()
        scope.launch { settleLate(pending.await(), began) }
    }

    private suspend fun restoreOutcome(): RestoreOutcome =
        runCatchingCancellable { TunnelBridge.restoreSession() }.fold(
            onSuccess = { RestoreOutcome.Restored(it) },
            onFailure = {
                if (isSignedOutRestore(coreErrorText(it))) RestoreOutcome.SignedOut else RestoreOutcome.Failed
            },
        )

    private suspend fun settle(outcome: RestoreOutcome, began: Int) {
        val current = began == generation
        if (current) {
            if (outcome is RestoreOutcome.Restored) open(outcome.session)
            _restoreFailed.value = outcome == RestoreOutcome.Failed
        }
        finishRestoring()
        if (!current) return
        when (outcome) {
            is RestoreOutcome.Restored -> fetchSubscription()
            // Nothing may keep a tunnel wanted that would only fail signed out.
            RestoreOutcome.SignedOut -> stopTunnel()
            RestoreOutcome.Failed -> Unit
        }
    }

    // The app is already open, so only a session the core no longer holds takes it away.
    private suspend fun settleLate(outcome: RestoreOutcome, began: Int) {
        if (began != generation) return
        when (outcome) {
            is RestoreOutcome.Restored -> {
                _session.value = outcome.session
                fetchSubscription()
            }
            RestoreOutcome.SignedOut -> endSession()
            RestoreOutcome.Failed -> {
                val held = holdsAccount()
                if (began != generation) return
                if (held) fetchSubscription() else endSession()
            }
        }
    }

    private fun finishRestoring() {
        _restoring.value = false
        _restored.value = true
    }

    private suspend fun attemptLogin(token: String) {
        _loggingIn.value = true
        _loginError.value = null
        val result = runCatchingCancellable { confirmedLogin(token) }
        _loggingIn.value = false
        val signedIn = result.getOrElse { return showLoginFailure(it) }
        pendingToken = null
        _showDeviceLimit.value = false
        _restoreFailed.value = false
        open(signedIn)
        fetchSubscription()
    }

    // The store cannot report a failed write, so a lost sign-in only shows on read-back.
    private suspend fun confirmedLogin(token: String): Session {
        val signedIn = TunnelBridge.login(token)
        if (holdsAccount()) return signedIn
        TunnelBridge.logout()
        throw SignInNotSaved()
    }

    // Other errors leave the device-limit screen alone: a failed retry stays on it and shows there.
    private fun showLoginFailure(error: Throwable) {
        val kind =
            if (error is SignInNotSaved) LoginErrorKind.SignInNotSaved else LoginErrors.kind(coreErrorText(error))
        if (kind == LoginErrorKind.DeviceLimit) _showDeviceLimit.value = true else _loginError.value = kind
    }

    // False when no answer came back: the hub refused, or was unreachable with nothing cached.
    private suspend fun fetchSubscription(): Boolean {
        val began = generation
        val answer = runCatchingCancellable { TunnelBridge.getSubscription() }
        if (answer.isFailure || began != generation) return false
        _subscription.value = answer.getOrNull()
        return true
    }

    // A start refused as signed out, with the core's account gone, means the hub ended the session.
    private suspend fun followCoreSignOut() {
        if (_session.value == null || _signingOut.value || _deletingAccount.value) return
        val began = generation
        if (holdsAccount() || began != generation) return
        endSession()
        _forcedSignOut.value = true
    }

    private suspend fun holdsAccount(): Boolean = TunnelBridge.accountNumber().isNotBlank()

    // Past the bound the call carries on in the core; nothing here waits out a dead network.
    private suspend fun retryPendingDeregister() {
        val call = scope.async { runCatchingCancellable { TunnelBridge.retryPendingDeregister() } }
        withTimeoutOrNull(DEREGISTER_MS) { call.await() }
    }

    private suspend fun sendClientInfoOnce() {
        if (clientInfoSent) return
        clientInfoSent = true
        @Suppress("DEPRECATION")
        val info = app.packageManager.getPackageInfo(app.packageName, 0)
        val version = "${info.versionName ?: "unknown"} (${PackageInfoCompat.getLongVersionCode(info)})"
        withContext(Dispatchers.IO) { TunnelBridge.setClientInfo(version, Build.VERSION.RELEASE) }
    }

    private fun open(session: Session) {
        if (_session.value == null) lastSubscriptionCheck = SystemClock.elapsedRealtime()
        _session.value = session
    }

    // Starting the service only to stop it is pointless, and a signed-out launch gets here every time.
    private fun stopTunnel() {
        val idle = TunnelBridge.status.value.state == ConnState.DISCONNECTED && !AppPrefs.value.tunnelWanted
        if (!idle) PangeaVpnService.disconnect(app)
    }

    private fun endSession() {
        stopTunnel()
        ServersRepository.reset()
        clear()
    }

    private fun clear() {
        generation++
        _session.value = null
        _restoreFailed.value = false
        _subscription.value = null
        _showDeviceLimit.value = false
        _loginError.value = null
        _forcedSignOut.value = false
        pendingToken = null
        lastSubscriptionCheck = null
    }
}
