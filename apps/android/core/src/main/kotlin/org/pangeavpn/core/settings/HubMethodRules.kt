package org.pangeavpn.core.settings

import org.pangeavpn.core.model.HUB_METHOD_CHOICES
import org.pangeavpn.core.model.HubMethods
import org.pangeavpn.core.model.HubStatus
import org.pangeavpn.core.model.HubTestResult
import org.pangeavpn.core.model.isEnabled

/** How a probe result reads: it worked, it had nothing to try, or the network failed it. */
enum class HubTestTone { OK, UNAVAILABLE, FAILED }

sealed interface HubTestOutcome {
    val tone: HubTestTone

    /** [via] is the address, relay or node the probe reached; empty when the core named none. */
    data class Works(val ms: Long, val via: String) : HubTestOutcome {
        override val tone: HubTestTone get() = HubTestTone.OK
    }

    data object Failed : HubTestOutcome {
        override val tone: HubTestTone get() = HubTestTone.FAILED
    }

    /** A method with nothing to try says why, rather than reading as a network failure. */
    enum class NothingToTry : HubTestOutcome {
        NO_ADDRESS, NO_REALITY_CREDENTIALS, NO_SHADOWSOCKS_CREDENTIALS, NO_RELAY, BUSY;

        override val tone: HubTestTone get() = HubTestTone.UNAVAILABLE
    }
}

sealed interface HubStatusLine {
    data object None : HubStatusLine

    /** [detail] is empty when the core gave none. */
    data class Using(val method: String, val detail: String) : HubStatusLine
}

object HubMethodRules {
    /** Desktop's order, which is the order the core tries them in. */
    val order: List<String> get() = HUB_METHOD_CHOICES

    fun enabled(methods: HubMethods): List<String> = order.filter { methods.isEnabled(it) }

    /** A tap still saving shows, and counts, as the value it asked for. */
    fun isOn(method: String, methods: HubMethods, pending: Map<String, Boolean>): Boolean =
        pending[method] ?: methods.isEnabled(method)

    /** The last method on cannot be switched off; pending taps count, so two quick ones cannot empty the set. */
    fun isLocked(method: String, methods: HubMethods, pending: Map<String, Boolean>): Boolean =
        isOn(method, methods, pending) && order.count { isOn(it, methods, pending) } == 1

    /** Testing a switched-off normal connection still puts our domain on the wire, so the page asks first. */
    fun testNeedsConfirmation(method: String, on: Boolean): Boolean = method == "normal" && !on

    fun outcome(result: HubTestResult): HubTestOutcome = when (result.unavailable) {
        "noAddress" -> HubTestOutcome.NothingToTry.NO_ADDRESS
        "noCredentials" ->
            if (result.method == "reality") HubTestOutcome.NothingToTry.NO_REALITY_CREDENTIALS
            else HubTestOutcome.NothingToTry.NO_SHADOWSOCKS_CREDENTIALS
        "noRelay" -> HubTestOutcome.NothingToTry.NO_RELAY
        "busy" -> HubTestOutcome.NothingToTry.BUSY
        else -> if (result.ok) HubTestOutcome.Works(result.ms, result.detail) else HubTestOutcome.Failed
    }

    fun statusLine(status: HubStatus): HubStatusLine {
        val active = status.active
        if (active.isNullOrEmpty()) return HubStatusLine.None
        return HubStatusLine.Using(active, status.detail.orEmpty())
    }
}
