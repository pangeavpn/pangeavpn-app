package org.pangeavpn.core.tunnel

/** What the tunnel tells connection alerts as it comes up, drops, recovers or stops. */
internal sealed interface TunnelEvent {
    data object Connected : TunnelEvent
    data object Reconnecting : TunnelEvent
    data object Restored : TunnelEvent
    data class Blocked(val reason: TunnelFailure) : TunnelEvent
    data object Stopped : TunnelEvent
}

// One notification tag per kind, so a newer alert replaces the older one of its kind.
internal enum class AlertKind {
    CONNECTED, LOST, RESTORED, BLOCKED;

    val tag: String get() = "org.pangeavpn.alert.${name.lowercase()}"
}

internal sealed interface ConnectionAlert {
    val kind: AlertKind

    data object Connected : ConnectionAlert {
        override val kind = AlertKind.CONNECTED
    }

    data object Lost : ConnectionAlert {
        override val kind = AlertKind.LOST
    }

    data object Restored : ConnectionAlert {
        override val kind = AlertKind.RESTORED
    }

    data class Blocked(val reason: TunnelFailure) : ConnectionAlert {
        override val kind = AlertKind.BLOCKED
    }
}

internal data class AlertGate(val enabled: Boolean, val appInForeground: Boolean) {
    val allowsAlerts: Boolean get() = enabled && !appInForeground
}

internal sealed interface AlertAction {
    data class Post(val alert: ConnectionAlert) : AlertAction
    data class Remove(val kinds: List<AlertKind>) : AlertAction
    data class ScheduleLost(val after: Double) : AlertAction
    data object CancelLost : AlertAction
}

/** iOS's AlertPolicy: decides which alerts an event deserves; the caller posts and times them. */
internal class AlertPolicy {
    private var lostPending = false
    private var outageShown = false
    private var blockedShown = false

    // Removals ignore the gate: they only clear alerts that no longer describe the tunnel.
    fun decide(event: TunnelEvent, gate: AlertGate): List<AlertAction> = when (event) {
        TunnelEvent.Reconnecting -> beginRecovering()
        TunnelEvent.Restored -> recover(gate, otherwise = null)
        // An in-app tap happens with the app open, so a start the gate lets through is a system one.
        TunnelEvent.Connected -> recover(gate, otherwise = ConnectionAlert.Connected)
        is TunnelEvent.Blocked -> block(event.reason, gate)
        TunnelEvent.Stopped -> end()
    }

    // The gate is read when the delay ends, so opening the app meanwhile still suppresses it.
    fun lostDelayElapsed(gate: AlertGate): List<AlertAction> {
        if (!lostPending) return emptyList()
        lostPending = false
        if (!gate.allowsAlerts) return emptyList()
        outageShown = true
        return listOf(AlertAction.Post(ConnectionAlert.Lost))
    }

    private fun beginRecovering(): List<AlertAction> {
        if (lostPending || outageShown) return emptyList()
        lostPending = true
        return listOf(AlertAction.ScheduleLost(LOST_DELAY))
    }

    private fun block(reason: TunnelFailure, gate: AlertGate): List<AlertAction> {
        val cancelled = cancelPendingLost()
        if (blockedShown || !gate.allowsAlerts) return cancelled
        blockedShown = true
        outageShown = true
        return cancelled + AlertAction.Remove(listOf(AlertKind.LOST)) + AlertAction.Post(ConnectionAlert.Blocked(reason))
    }

    // A start clears alerts a previous run may have left behind.
    private fun recover(gate: AlertGate, otherwise: ConnectionAlert?): List<AlertAction> {
        val announced = outageShown
        val actions = cancelPendingLost().toMutableList()
        reset()
        if (announced || otherwise != null) actions += AlertAction.Remove(OUTAGE_KINDS)
        val alert = if (announced) ConnectionAlert.Restored else otherwise
        if (gate.allowsAlerts && alert != null) actions += AlertAction.Post(alert)
        return actions
    }

    private fun end(): List<AlertAction> {
        val cancelled = cancelPendingLost()
        reset()
        return cancelled + AlertAction.Remove(OUTAGE_KINDS)
    }

    private fun cancelPendingLost(): List<AlertAction> {
        if (!lostPending) return emptyList()
        lostPending = false
        return listOf(AlertAction.CancelLost)
    }

    private fun reset() {
        lostPending = false
        outageShown = false
        blockedShown = false
    }

    companion object {
        // Long enough that quick blips and in-place switches recover before anyone is told.
        const val LOST_DELAY = 10.0
        private val OUTAGE_KINDS = listOf(AlertKind.LOST, AlertKind.BLOCKED)
    }
}
