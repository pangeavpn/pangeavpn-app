package org.pangeavpn.core.data

import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.Session
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.util.normalizeAccountNumber

// Matches iOS: one hub search plus the request, and a short wait for the tunnel to go.
private const val DEREGISTER_TIMEOUT_MS = 25_000L
private const val TUNNEL_DOWN_TIMEOUT_MS = 5_000L

/** Thin cache over [TunnelBridge] so screens share one session/server list. */
object SessionRepository {
    private val _session = MutableStateFlow<Session?>(null)
    val session: StateFlow<Session?> = _session.asStateFlow()

    val servers: List<Server> get() = _session.value?.servers.orEmpty()

    suspend fun login(token: String): Session {
        val result = TunnelBridge.login(normalizeAccountNumber(token))
        _session.value = result
        return result
    }

    suspend fun restore(): Session {
        val result = TunnelBridge.restoreSession()
        _session.value = result
        return result
    }

    /**
     * Frees this device's slot on the hub, then forgets the session. The tunnel is
     * given a moment to go down first, so the request leaves on the real network.
     * Not cancellable: a screen closing mid-way must not leave the account signed in.
     */
    suspend fun signOut(): Unit = withContext(NonCancellable) {
        if (_session.value != null) {
            withTimeoutOrNull(TUNNEL_DOWN_TIMEOUT_MS) {
                TunnelBridge.status.first { it.state == ConnState.DISCONNECTED || it.state == ConnState.ERROR }
            }
            TunnelBridge.deregister(DEREGISTER_TIMEOUT_MS)
        }
        TunnelBridge.logout()
        _session.value = null
    }

    suspend fun refreshServers(): List<Server> {
        val servers = TunnelBridge.listServers()
        _session.value = _session.value?.copy(servers = servers)
        return servers
    }

    suspend fun subscription(): Subscription? = TunnelBridge.getSubscription()

    suspend fun devices(): List<Device> = TunnelBridge.listDevices()

    suspend fun removeDevice(id: String) = TunnelBridge.removeDevice(id)
}
