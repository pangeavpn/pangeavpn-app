package org.pangeavpn.core

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class RoutesTest {

    @Test
    fun `default route captures everything when allow lan is off`() {
        assertEquals(listOf(Route("0.0.0.0", 0)), tunnelRoutes(allowLan = false))
    }

    @Test
    fun `allow lan installs the complement instead of a default route`() {
        val routes = tunnelRoutes(allowLan = true)
        assertFalse(
            routes.any { it.prefixLength == 0 },
            "a default route would swallow the LAN ranges we just excluded",
        )
        assertTrue(routes.isNotEmpty())
    }

    @Test
    fun `allow lan routes cover every address outside the lan ranges`() {
        val routes = tunnelRoutes(allowLan = true)
        val covered = routes.map { it.span() }

        // Public addresses must still be tunnelled.
        for (address in listOf("1.1.1.1", "8.8.8.8", "203.0.113.10", "172.32.0.1", "11.0.0.1")) {
            val value = ipToLong(address)
            assertTrue(
                covered.any { value >= it.first && value <= it.second },
                "$address should be routed into the tunnel",
            )
        }
    }

    @Test
    fun `allow lan routes never overlap a lan range`() {
        val routes = tunnelRoutes(allowLan = true)
        val covered = routes.map { it.span() }

        for (address in listOf("10.0.0.1", "10.255.255.254", "172.16.0.1", "172.31.255.254", "192.168.1.1", "169.254.1.1")) {
            val value = ipToLong(address)
            assertFalse(
                covered.any { value >= it.first && value <= it.second },
                "$address is LAN and must bypass the tunnel",
            )
        }
    }

    @Test
    fun `allow lan routes do not overlap each other`() {
        val spans = tunnelRoutes(allowLan = true).map { it.span() }.sortedBy { it.first }
        for (index in 1 until spans.size) {
            assertTrue(
                spans[index].first > spans[index - 1].second,
                "routes ${spans[index - 1]} and ${spans[index]} overlap",
            )
        }
    }

    @Test
    fun `every emitted route is CIDR aligned`() {
        for (route in tunnelRoutes(allowLan = true)) {
            val size = 1L shl (32 - route.prefixLength)
            assertEquals(
                0L,
                ipToLong(route.address) % size,
                "${route.address}/${route.prefixLength} is not aligned to its block size",
            )
        }
    }

    @Test
    fun `ip conversion round trips`() {
        for (address in listOf("0.0.0.0", "1.2.3.4", "192.168.1.1", "255.255.255.255")) {
            assertEquals(address, longToIp(ipToLong(address)))
        }
    }

    @Test
    fun `excluding nothing returns the base range`() {
        assertEquals(listOf(Route("0.0.0.0", 0)), excludeRanges(Route("0.0.0.0", 0), emptyList()))
    }

    private fun Route.span(): Pair<Long, Long> {
        val start = ipToLong(address)
        return start to (start + (1L shl (32 - prefixLength)) - 1)
    }
}

class RoutesV6Test {

    @Test
    fun `ipv6 conversion round trips`() {
        for (address in listOf("::", "fc00::", "fe80::", "2001:4860:4860::8888", "::1")) {
            assertEquals(
                ipv6ToBigInteger(address),
                ipv6ToBigInteger(bigIntegerToIpv6(ipv6ToBigInteger(address))),
            )
        }
    }

    @Test
    fun `a single default route captures all ipv6 when allow lan is off`() {
        val routes = tunnelRoutesV6(allowLan = false)
        assertEquals(1, routes.size)
        assertEquals(0, routes.first().prefixLength)
        assertEquals(java.math.BigInteger.ZERO, ipv6ToBigInteger(routes.first().address))
    }

    @Test
    fun `allow lan still captures public ipv6 but not the local ranges`() {
        val covered = tunnelRoutesV6(allowLan = true).map { it.span() }

        for (address in listOf("2001:4860:4860::8888", "2606:4700:4700::1111", "::1")) {
            assertTrue(covered.any { it.contains(address) }, "$address should be routed into the tunnel")
        }
        for (address in listOf("fc00::1", "fd12:3456::1", "fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff", "fe80::1")) {
            assertFalse(covered.any { it.contains(address) }, "$address is local and must bypass the tunnel")
        }
    }

    @Test
    fun `ipv6 routes never overlap each other`() {
        val spans = tunnelRoutesV6(allowLan = true).map { it.span() }.sortedBy { it.first }
        for (index in 1 until spans.size) {
            assertTrue(spans[index].first > spans[index - 1].second, "routes overlap at index $index")
        }
    }

    @Test
    fun `every emitted ipv6 route is CIDR aligned`() {
        for (route in tunnelRoutesV6(allowLan = true)) {
            val size = java.math.BigInteger.TWO.pow(128 - route.prefixLength)
            assertEquals(
                java.math.BigInteger.ZERO,
                ipv6ToBigInteger(route.address).mod(size),
                "${route.address}/${route.prefixLength} is not aligned to its block size",
            )
        }
    }

    private fun Route.span(): Pair<java.math.BigInteger, java.math.BigInteger> {
        val start = ipv6ToBigInteger(address)
        return start to (start + java.math.BigInteger.TWO.pow(128 - prefixLength) - java.math.BigInteger.ONE)
    }

    private fun Pair<java.math.BigInteger, java.math.BigInteger>.contains(address: String): Boolean {
        val value = ipv6ToBigInteger(address)
        return value >= first && value <= second
    }
}
