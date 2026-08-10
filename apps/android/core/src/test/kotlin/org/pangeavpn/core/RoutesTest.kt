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
