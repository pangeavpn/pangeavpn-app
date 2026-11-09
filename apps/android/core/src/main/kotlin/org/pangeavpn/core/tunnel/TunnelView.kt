package org.pangeavpn.core.tunnel

import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.TunnelConfig
import org.pangeavpn.core.model.TunnelStatus

/** What the service lays over the core's status: its phase decides state and detail, the core the rest. */
internal data class TunnelView(
    val state: ConnState,
    val detail: String = "",
    val failure: TunnelFailure? = null,
    val reconnecting: Boolean = false,
    val connectedSince: Long = 0,
    // The route last landed on, named while the core has none or is between servers.
    val route: TunnelConfig? = null,
) {
    fun applyTo(core: TunnelStatus): TunnelStatus {
        val named = if (route != null && (state != ConnState.CONNECTED || core.serverId.isEmpty())) {
            core.copy(
                serverId = route.serverId,
                serverName = route.serverName,
                entryServerId = route.entryServerId,
                entryServerName = route.entryServerName,
            )
        } else {
            core
        }
        return named.copy(state = state, detail = detail, reconnecting = reconnecting, connectedSince = connectedSince)
    }
}
