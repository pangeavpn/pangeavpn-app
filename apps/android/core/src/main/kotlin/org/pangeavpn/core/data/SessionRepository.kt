package org.pangeavpn.core.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.Session
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.util.normalizeAccountNumber

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

    suspend fun logout() {
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
