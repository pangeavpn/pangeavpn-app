package org.pangeavpn.core.tunnel

internal sealed interface PathDecision {
    data object Ignore : PathDecision
    data class Reconnect(val after: Double) : PathDecision
    data object WaitForNetwork : PathDecision
    data class Recheck(val after: Double) : PathDecision
}

internal sealed interface RetryKind {
    data object Transient : RetryKind
    data class Terminal(val failure: TunnelFailure) : RetryKind
}

/** iOS's ReconnectPolicy; times are seconds on the uptime clock. */
internal class ReconnectPolicy {
    private var lastKey: String? = null
    private var waiting = false
    private var appliedAt: Double? = null

    fun backoff(attempt: Int): Double =
        if (attempt > STEPPED_BACKOFF.size) STEADY_BACKOFF else STEPPED_BACKOFF[maxOf(attempt, 1) - 1]

    fun delay(after: RetryKind, attempt: Int): Double = when (after) {
        RetryKind.Transient -> backoff(attempt)
        is RetryKind.Terminal -> TERMINAL_BACKOFF
    }

    fun rotates(afterFailedAttempt: Int): Boolean =
        afterFailedAttempt > 0 && afterFailedAttempt % ROTATE_EVERY == 0

    fun seed(key: String) {
        if (key.isNotEmpty()) lastKey = key
    }

    fun settingsApplied(at: Double) {
        appliedAt = at
    }

    fun markWaiting() {
        waiting = true
    }

    fun pathChanged(key: String, usable: Boolean, now: Double): PathDecision {
        if (!usable) {
            graceLeft(now)?.let { return PathDecision.Recheck(it) }
            waiting = true
            return PathDecision.WaitForNetwork
        }
        if (key.isEmpty()) return PathDecision.Ignore
        val previous = lastKey
        lastKey = key
        if (waiting) {
            waiting = false
            return PathDecision.Reconnect(DEBOUNCE)
        }
        if (previous == null || previous == key) return PathDecision.Ignore
        return PathDecision.Reconnect(DEBOUNCE)
    }

    private fun graceLeft(now: Double): Double? {
        val applied = appliedAt ?: return null
        val left = applied + SETTLE_GRACE - now
        return if (left > 0) left else null
    }

    companion object {
        const val DEBOUNCE = 1.5

        // A path blip this close to our own TUN change is probably ours, so it is checked again later.
        const val SETTLE_GRACE = 5.0
        private val STEPPED_BACKOFF = listOf(3.0, 6.0, 9.0)
        const val STEADY_BACKOFF = 30.0

        // Retrying a signed-out or expired account any sooner only hammers the hub.
        const val TERMINAL_BACKOFF = 300.0
        const val ROTATE_EVERY = 3

        private val TERMINAL = setOf(
            TunnelFailure.SIGNED_OUT,
            TunnelFailure.SUBSCRIPTION_EXPIRED,
            TunnelFailure.NO_SUBSCRIPTION,
            TunnelFailure.NO_SERVER,
        )

        fun classify(failure: TunnelFailure): RetryKind =
            if (failure in TERMINAL) RetryKind.Terminal(failure) else RetryKind.Transient
    }
}

/** The servers a session may use, best first; the current one is tried until rotated away from. */
internal class CandidateList private constructor(val ids: List<String>, val index: Int) {
    val isEmpty: Boolean get() = ids.isEmpty()

    val current: String get() = ids.getOrElse(index) { "" }

    fun at(position: Int): CandidateList = of(ids, position)

    fun advanced(): CandidateList = if (ids.isEmpty()) this else of(ids, (index + 1) % ids.size)

    override fun equals(other: Any?): Boolean = other is CandidateList && other.ids == ids && other.index == index

    override fun hashCode(): Int = 31 * ids.hashCode() + index

    companion object {
        fun of(ids: List<String>, index: Int = 0): CandidateList =
            CandidateList(ids, if (ids.isEmpty()) 0 else index.coerceIn(0, ids.size - 1))

        // The new server leads and the one being left is the first fallback.
        fun forSwitch(target: String, previous: String, listed: List<String>): CandidateList =
            of((listOf(target, previous) + listed).filter { it.isNotEmpty() }.distinct())
    }
}
