package org.pangeavpn.core.tunnel

import org.pangeavpn.core.R

/** Why a start or reconnect failed, mirroring iOS TunnelStartError. */
enum class TunnelFailure {
    NO_SERVER,
    SIGNED_OUT,
    NO_TUNNEL_INTERFACE,
    STOPPED,
    DEVICE_LOCKED,
    SUBSCRIPTION_EXPIRED,
    NO_SUBSCRIPTION,
    CONNECT_FAILED,
    INVALID_DNS,
    VPN_PERMISSION,
    ;

    /** The sentence to show for it, from :core's strings. */
    val messageRes: Int
        get() = when (this) {
            NO_SERVER -> R.string.error_no_server
            SIGNED_OUT -> R.string.error_signed_out
            NO_TUNNEL_INTERFACE -> R.string.error_no_tun
            STOPPED -> R.string.error_stopped
            DEVICE_LOCKED -> R.string.error_device_locked
            SUBSCRIPTION_EXPIRED -> R.string.error_subscription_expired
            NO_SUBSCRIPTION -> R.string.error_no_subscription
            CONNECT_FAILED -> R.string.error_connect_failed
            INVALID_DNS -> R.string.error_dns_invalid
            VPN_PERMISSION -> R.string.error_vpn_permission
        }

    companion object {
        // First match wins, so the most specific markers come first.
        private val CORE_MARKERS = listOf(
            "SUBSCRIPTION_EXPIRED" to SUBSCRIPTION_EXPIRED,
            "NO_SUBSCRIPTION" to NO_SUBSCRIPTION,
            "unknown server" to NO_SERVER,
            "no stored session" to SIGNED_OUT,
            "no device identity" to SIGNED_OUT,
            "not authenticated" to SIGNED_OUT,
            "hub auth error" to SIGNED_OUT,
            "DEVICE_NOT_REGISTERED" to SIGNED_OUT,
        )

        /** Core text can carry dial errors and addresses, so the user only ever sees a mapped case. */
        fun fromCoreMessage(message: String): TunnelFailure =
            CORE_MARKERS.firstOrNull { message.contains(it.first) }?.second ?: CONNECT_FAILED

        internal fun of(error: Throwable): TunnelFailure =
            (error as? TunnelStartException)?.failure ?: fromCoreMessage(error.message.orEmpty())
    }
}

/** A start step that failed for a reason of the tunnel's own rather than the core's. */
internal class TunnelStartException(val failure: TunnelFailure, cause: Throwable? = null) :
    Exception(failure.name, cause)
