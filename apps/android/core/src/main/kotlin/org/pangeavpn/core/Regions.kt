package org.pangeavpn.core

import kotlin.random.Random
import org.pangeavpn.core.model.Server

/** A region groups the nodes serving one location. Ports
 *  apps/desktop/src/renderer/{regions,serverPick}.ts. */
data class Region(
    val key: String,
    val name: String,
    val country: String,
    val nodes: List<Server>,
)

private val NODE_SUFFIX = Regex("-(\\d+)$")

/** Servers at or below this load are preferred by a random pick. */
const val PREFERRED_MAX_LOAD = 75

/** `eu-central-1` becomes `eu-central`; ids without a suffix stand alone. */
fun regionKeyOf(server: Server): String = server.id.replace(NODE_SUFFIX, "")

/** Groups servers into regions, preserving first-seen order. */
fun groupRegions(servers: List<Server>): List<Region> {
    val byKey = LinkedHashMap<String, Region>()
    for (server in servers) {
        val key = regionKeyOf(server)
        val existing = byKey[key]
        byKey[key] = existing?.copy(nodes = existing.nodes + server)
            ?: Region(
                key = key,
                name = server.name.ifEmpty { key },
                country = server.country,
                nodes = listOf(server),
            )
    }
    return byKey.values.toList()
}

/** Missing load counts as fully loaded, so a node that reports always wins. */
fun loadOf(server: Server): Int = server.load ?: 100

/** Lowest-load node in the region. */
fun pickNode(region: Region): Server =
    region.nodes.minByOrNull { loadOf(it) } ?: region.nodes.first()

/** The load a region actually offers: that of the node we would pick. */
fun regionLoad(region: Region): Int? = pickNode(region).load

fun regionOfServer(regions: List<Region>, serverId: String): Region? =
    regions.firstOrNull { region -> region.nodes.any { it.id == serverId } }

/**
 * Uniform pick from the lightly loaded servers, falling back to all of them
 * when every server is congested or none reports load. Deliberately not
 * load-proportional, so clients do not herd onto one node.
 */
fun pickRandomServer(servers: List<Server>, random: Random = Random.Default): Server? {
    if (servers.isEmpty()) return null
    val light = servers.filter { server -> server.load?.let { it <= PREFERRED_MAX_LOAD } == true }
    val pool = light.ifEmpty { servers }
    return pool[random.nextInt(pool.size)]
}

/**
 * Which server the picker shows: the in-session pick if still listed, else the
 * last connected one if still listed, else nothing — so the picker reads
 * "Select server" and Connect rolls a random one.
 */
fun resolveSelection(visible: List<Server>, previousValue: String, lastServerId: String?): String {
    if (visible.any { it.id == previousValue }) return previousValue
    if (lastServerId != null && visible.any { it.id == lastServerId }) return lastServerId
    return ""
}
