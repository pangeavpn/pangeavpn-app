package org.pangeavpn.core

/** One IPv4 route in CIDR form. */
data class Route(val address: String, val prefixLength: Int)

/** Ranges kept out of the tunnel when Allow LAN is on: RFC1918 plus link-local,
 *  so routers, printers and captive portals stay reachable. */
val LAN_RANGES = listOf(
    Route("10.0.0.0", 8),
    Route("172.16.0.0", 12),
    Route("192.168.0.0", 16),
    Route("169.254.0.0", 16),
)

private val DEFAULT_ROUTE = Route("0.0.0.0", 0)

/**
 * The routes to install in the tunnel. Android has no "exclude this range", so
 * bypassing the LAN means installing the complement of those ranges instead of
 * a single default route.
 */
fun tunnelRoutes(allowLan: Boolean): List<Route> {
    if (!allowLan) return listOf(DEFAULT_ROUTE)
    return excludeRanges(DEFAULT_ROUTE, LAN_RANGES)
}

/**
 * Splits base into the largest CIDR blocks that cover it without overlapping
 * any excluded range.
 */
internal fun excludeRanges(base: Route, excluded: List<Route>): List<Route> {
    val holes = excluded.map { it.toRange() }.sortedBy { it.first }
    val out = mutableListOf<Route>()
    var cursor = base.toRange().first
    val end = base.toRange().second

    for (hole in holes) {
        if (hole.second < cursor) continue
        if (hole.first > cursor) {
            out += rangeToCidrs(cursor, hole.first - 1)
        }
        cursor = maxOf(cursor, hole.second + 1)
        if (cursor > end) break
    }
    if (cursor <= end) {
        out += rangeToCidrs(cursor, end)
    }
    return out
}

/** Greedily covers [start, end] with aligned CIDR blocks. */
private fun rangeToCidrs(start: Long, end: Long): List<Route> {
    val out = mutableListOf<Route>()
    var current = start
    while (current <= end) {
        var prefix = 32
        // Grow the block while it stays aligned and still fits.
        while (prefix > 0) {
            val candidate = prefix - 1
            val size = 1L shl (32 - candidate)
            if (current % size != 0L || current + size - 1 > end) break
            prefix = candidate
        }
        out += Route(longToIp(current), prefix)
        current += 1L shl (32 - prefix)
    }
    return out
}

private fun Route.toRange(): Pair<Long, Long> {
    val start = ipToLong(address)
    val size = 1L shl (32 - prefixLength)
    return start to (start + size - 1)
}

internal fun ipToLong(address: String): Long =
    address.split(".").fold(0L) { acc, octet -> (acc shl 8) or (octet.toLong() and 0xFF) }

internal fun longToIp(value: Long): String =
    listOf(24, 16, 8, 0).joinToString(".") { shift -> ((value shr shift) and 0xFF).toString() }
