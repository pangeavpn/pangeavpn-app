package org.pangeavpn.core.connection

import org.pangeavpn.core.model.ConnState

/** What the hero's detail line says; `ui.text.heroDetailText` words it. */
sealed interface HeroDetailLine {
    data object Paused : HeroDetailLine
    data class Progress(val step: ConnectProgress) : HeroDetailLine
    data class Text(val text: String) : HeroDetailLine
}

object HeroRules {
    /** A start or switch in flight reads as connecting before the tunnel itself says so. */
    fun display(state: ConnState, busy: Boolean, switching: Boolean): ConnState = when {
        switching && state == ConnState.CONNECTED -> ConnState.CONNECTING
        busy && isIdle(state) -> ConnState.CONNECTING
        else -> state
    }

    fun isUp(state: ConnState): Boolean = state == ConnState.CONNECTED || state == ConnState.CONNECTING

    /** Lockdown keeps the tunnel up until Lockdown itself is turned off. */
    fun lockdownBlocks(lockdown: Boolean, state: ConnState): Boolean = lockdown && isUp(state)

    /** Only an idle hero has room for a connect error; anything later goes to a toast. */
    fun carriesError(display: ConnState): Boolean = isIdle(display)

    /** Idle has no session to report, so the facts give their room to more regions. */
    fun showsFacts(display: ConnState): Boolean = !isIdle(display)

    /** [paused] is the service's own `status.reconnecting`: the routes still block, so the internet waits. */
    fun detail(
        display: ConnState,
        statusDetail: String,
        serverName: String,
        error: String?,
        progress: ConnectProgress?,
        paused: Boolean,
    ): HeroDetailLine = when {
        paused -> HeroDetailLine.Paused
        display == ConnState.CONNECTING && progress != null -> HeroDetailLine.Progress(progress)
        carriesError(display) && !error.isNullOrEmpty() -> HeroDetailLine.Text(error)
        else -> HeroDetailLine.Text(statusDetail.ifEmpty { serverName })
    }

    fun showsPostQuantum(display: ConnState, postQuantum: Boolean): Boolean =
        display == ConnState.CONNECTED && postQuantum

    /** Android has no strict kill switch to void a split; excluded apps count like excluded ranges. */
    fun showsSplit(display: ConnState, enabled: Boolean, hasExclusions: Boolean): Boolean =
        display == ConnState.CONNECTED && enabled && hasExclusions

    private fun isIdle(state: ConnState): Boolean = state == ConnState.DISCONNECTED || state == ConnState.ERROR
}

enum class MainAction {
    CONNECT, PROVISIONING, STOP, DISCONNECT, DISCONNECTING, LOCKDOWN, HIDDEN;

    val isButton: Boolean get() = this != LOCKDOWN && this != HIDDEN

    companion object {
        /** Lockdown hides every way to stop the tunnel; a tap is ignored while a start is in flight. */
        fun resolve(
            state: ConnState,
            busy: Boolean,
            switching: Boolean,
            reconnecting: Boolean,
            lockdownBlocks: Boolean,
            entitled: Boolean,
        ): MainAction = when (state) {
            ConnState.DISCONNECTING -> MainAction.DISCONNECTING
            ConnState.CONNECTED, ConnState.CONNECTING -> when {
                lockdownBlocks -> MainAction.LOCKDOWN
                busy -> MainAction.PROVISIONING
                reconnecting || (state == ConnState.CONNECTED && !switching) -> MainAction.DISCONNECT
                else -> MainAction.STOP
            }
            ConnState.DISCONNECTED, ConnState.ERROR -> when {
                busy -> MainAction.PROVISIONING
                entitled -> MainAction.CONNECT
                else -> MainAction.HIDDEN
            }
        }
    }
}
