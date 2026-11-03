package org.pangeavpn.core

import java.math.BigInteger

/** One route in CIDR form, either family. */
data class Route(val address: String, val prefixLength: Int)

/** Ranges kept out of the tunnel when Allow LAN is on: RFC1918 plus link-local,
 *  so routers, printers and captive portals stay reachable. */
val LAN_RANGES = listOf(
    Route("10.0.0.0", 8),
    Route("172.16.0.0", 12),
    Route("192.168.0.0", 16),
    Route("169.254.0.0", 16),
)

/** The IPv6 equivalents: unique-local and link-local. */
val LAN_RANGES_V6 = listOf(
    Route("fc00::", 7),
    Route("fe80::", 10),
)

private val DEFAULT_ROUTE = Route("0.0.0.0", 0)
private val DEFAULT_ROUTE_V6 = Route("::", 0)

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
 * The IPv6 routes. Nothing carries IPv6 yet, so these exist to capture it and
 * let WireGuard drop it — left uncaptured it would leave the device outside the
 * tunnel. See [PangeaVpnService] for why an address must accompany them.
 */
fun tunnelRoutesV6(allowLan: Boolean): List<Route> {
    if (!allowLan) return listOf(DEFAULT_ROUTE_V6)
    return excludeRangesIn(IPV6, DEFAULT_ROUTE_V6, LAN_RANGES_V6)
}

/**
 * Splits base into the largest CIDR blocks that cover it without overlapping
 * any excluded range.
 */
internal fun excludeRanges(base: Route, excluded: List<Route>): List<Route> =
    excludeRangesIn(IPV4, base, excluded)

/** How one address family converts between text and a plain integer. */
private class IpFamily(
    val bits: Int,
    val parse: (String) -> BigInteger,
    val format: (BigInteger) -> String,
)

private val IPV4 = IpFamily(32, { BigInteger.valueOf(ipToLong(it)) }, { longToIp(it.toLong()) })
private val IPV6 = IpFamily(128, ::ipv6ToBigInteger, ::bigIntegerToIpv6)

private fun excludeRangesIn(family: IpFamily, base: Route, excluded: List<Route>): List<Route> {
    val holes = excluded.map { it.toRange(family) }.sortedBy { it.first }
    val out = mutableListOf<Route>()
    val (start, end) = base.toRange(family)
    var cursor = start

    for (hole in holes) {
        if (hole.second < cursor) continue
        if (hole.first > cursor) {
            out += rangeToCidrs(family, cursor, hole.first - BigInteger.ONE)
        }
        cursor = cursor.max(hole.second + BigInteger.ONE)
        if (cursor > end) break
    }
    if (cursor <= end) {
        out += rangeToCidrs(family, cursor, end)
    }
    return out
}

/** Greedily covers [start, end] with aligned CIDR blocks. */
private fun rangeToCidrs(family: IpFamily, start: BigInteger, end: BigInteger): List<Route> {
    val out = mutableListOf<Route>()
    var current = start
    while (current <= end) {
        var prefix = family.bits
        // Grow the block while it stays aligned and still fits.
        while (prefix > 0) {
            val size = BigInteger.TWO.pow(family.bits - (prefix - 1))
            if (current.mod(size).signum() != 0 || current + size - BigInteger.ONE > end) break
            prefix -= 1
        }
        out += Route(family.format(current), prefix)
        current += BigInteger.TWO.pow(family.bits - prefix)
    }
    return out
}

private fun Route.toRange(family: IpFamily): Pair<BigInteger, BigInteger> {
    val start = family.parse(address)
    return start to (start + BigInteger.TWO.pow(family.bits - prefixLength) - BigInteger.ONE)
}

internal fun ipToLong(address: String): Long =
    address.split(".").fold(0L) { acc, octet -> (acc shl 8) or (octet.toLong() and 0xFF) }

internal fun longToIp(value: Long): String =
    listOf(24, 16, 8, 0).joinToString(".") { shift -> ((value shr shift) and 0xFF).toString() }

/** Literal-only parser: the addresses here are our own constants, never input. */
internal fun ipv6ToBigInteger(address: String): BigInteger {
    val head: List<String>
    val tail: List<String>
    if (address.contains("::")) {
        val halves = address.split("::", limit = 2)
        head = halves[0].split(":").filter { it.isNotEmpty() }
        tail = halves[1].split(":").filter { it.isNotEmpty() }
    } else {
        head = address.split(":")
        tail = emptyList()
    }
    val groups = head + List(GROUP_COUNT - head.size - tail.size) { "0" } + tail
    return groups.fold(BigInteger.ZERO) { acc, group ->
        acc.shiftLeft(GROUP_BITS) + BigInteger(group, 16)
    }
}

/** Emits the uncompressed form, which Android's address parser accepts. */
internal fun bigIntegerToIpv6(value: BigInteger): String =
    (GROUP_COUNT - 1 downTo 0).joinToString(":") { group ->
        val chunk = value.shiftRight(group * GROUP_BITS).and(GROUP_MASK)
        "%04x".format(chunk.toInt())
    }

private const val GROUP_COUNT = 8
private const val GROUP_BITS = 16
private val GROUP_MASK = BigInteger.valueOf(0xFFFF)
