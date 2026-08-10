package org.pangeavpn.core

import kotlin.random.Random
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue
import org.pangeavpn.core.model.Server

private fun server(id: String, load: Int? = null, name: String = "Node", country: String = "GB") =
    Server(id = id, name = name, region = "eu", country = country, load = load)

class RegionsTest {

    @Test
    fun `region key strips the numeric node suffix`() {
        assertEquals("eu-central", regionKeyOf(server("eu-central-1")))
        assertEquals("eu-central", regionKeyOf(server("eu-central-12")))
        assertEquals("standalone", regionKeyOf(server("standalone")))
    }

    @Test
    fun `grouping preserves first seen order and collects nodes`() {
        val regions = groupRegions(
            listOf(server("eu-1"), server("us-1"), server("eu-2")),
        )
        assertEquals(listOf("eu", "us"), regions.map { it.key })
        assertEquals(2, regions.first().nodes.size)
    }

    @Test
    fun `a node that reports load beats one that does not`() {
        val region = groupRegions(listOf(server("eu-1", load = null), server("eu-2", load = 90))).first()
        assertEquals("eu-2", pickNode(region).id)
    }

    @Test
    fun `region picks its lowest load node`() {
        val region = groupRegions(listOf(server("eu-1", 80), server("eu-2", 10), server("eu-3", 40))).first()
        assertEquals("eu-2", pickNode(region).id)
        assertEquals(10, regionLoad(region))
    }

    @Test
    fun `region load is null when no node reports`() {
        val region = groupRegions(listOf(server("eu-1"), server("eu-2"))).first()
        assertNull(regionLoad(region))
    }

    @Test
    fun `random pick prefers lightly loaded servers`() {
        val servers = listOf(server("a", 10), server("b", 90), server("c", 20))
        repeat(50) { seed ->
            val picked = pickRandomServer(servers, Random(seed))
            assertTrue(picked?.id in setOf("a", "c"), "congested node b should not be picked")
        }
    }

    @Test
    fun `random pick falls back when everything is congested`() {
        val servers = listOf(server("a", 90), server("b", 95))
        assertTrue(pickRandomServer(servers, Random(1))?.id in setOf("a", "b"))
    }

    @Test
    fun `random pick falls back when nothing reports load`() {
        val servers = listOf(server("a"), server("b"))
        assertTrue(pickRandomServer(servers, Random(1))?.id in setOf("a", "b"))
    }

    @Test
    fun `random pick of an empty list is null`() {
        assertNull(pickRandomServer(emptyList()))
    }

    @Test
    fun `selection keeps the in-session pick while it is listed`() {
        val visible = listOf(server("a"), server("b"))
        assertEquals("b", resolveSelection(visible, "b", "a"))
    }

    @Test
    fun `selection falls back to the last connected server`() {
        val visible = listOf(server("a"), server("b"))
        assertEquals("a", resolveSelection(visible, "gone", "a"))
    }

    @Test
    fun `selection empties when neither is listed so connect rolls a random one`() {
        val visible = listOf(server("a"))
        assertEquals("", resolveSelection(visible, "gone", "also-gone"))
    }

    @Test
    fun `region of server finds the owning region`() {
        val regions = groupRegions(listOf(server("eu-1"), server("us-1")))
        assertEquals("us", regionOfServer(regions, "us-1")?.key)
        assertNull(regionOfServer(regions, "nope"))
    }
}
