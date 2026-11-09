package org.pangeavpn.core.connection

/** Reassurance under a slow connect, timed from when the tunnel started connecting. */
enum class ConnectProgress(val afterMs: Long) {
    STILL_CONNECTING(3_000),
    TAKING_LONGER(8_000),
    STILL_WORKING(15_000);

    /** How long to wait before this step, measured from the one before it. */
    val waitMs: Long get() = afterMs - (ConnectProgress.entries.getOrNull(ordinal - 1)?.afterMs ?: 0)

    companion object {
        fun after(elapsedMs: Long): ConnectProgress? = ConnectProgress.entries.lastOrNull { it.afterMs <= elapsedMs }
    }
}
