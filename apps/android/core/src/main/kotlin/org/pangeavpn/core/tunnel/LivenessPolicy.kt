package org.pangeavpn.core.tunnel

internal enum class Liveness { UNKNOWN, GRACE, HEALTHY, STALE }

// Persistent keepalive is 25 s, so a live tunnel re-handshakes about every two minutes.
internal data class LivenessPolicy(val graceUntil: Double? = null) {
    fun armed(now: Double, grace: Double = GRACE): LivenessPolicy = LivenessPolicy(now + grace)

    // lastHandshake and wallNow are unix seconds; now is the uptime clock the grace was armed on.
    fun verdict(lastHandshake: Long, wallNow: Double, now: Double): Liveness {
        if (lastHandshake <= 0) return Liveness.UNKNOWN
        if (graceUntil != null && now < graceUntil) return Liveness.GRACE
        return if (wallNow - lastHandshake.toDouble() > STALE_AFTER) Liveness.STALE else Liveness.HEALTHY
    }

    companion object {
        const val CHECK_INTERVAL = 15.0
        const val STALE_AFTER = 180.0
        const val GRACE = 30.0

        // Traffic or the keepalive re-handshakes within seconds of a wake, so a dead tunnel is found fast.
        const val WAKE_GRACE = 15.0
        val WAKE_CHECKS = listOf(17.0, 32.0)
    }
}
