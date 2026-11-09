package org.pangeavpn.core

import kotlin.random.Random
import org.pangeavpn.core.model.Server

const val AUTO_TRANSPORT = "auto"

/** How many regions get a row of their own before the rest go behind "All regions". */
const val REGION_SLOT_COUNT = 2

/** How many servers one start or switch may fall back through, the chosen one included. */
const val CANDIDATE_LIMIT = 6

const val MAX_RECENT_REGIONS = 8

/** Servers at or below this load are preferred by a random pick. */
const val PREFERRED_MAX_LOAD = 75

private val NODE_SUFFIX = Regex("-[0-9]+$")
private val NAME_ORDINAL = Regex("\\s+[0-9]+$")

/** The nodes serving one location, in hub order. */
data class Region(val key: String, val servers: List<Server>) {
    /** Missing load counts as fully loaded, so a node that reports always wins. */
    val best: Server get() = servers.minBy { loadOf(it) }

    /** The first named node without its ordinal, so a region never reads as one node ("Amsterdam 1"). */
    val name: String
        get() = servers.firstOrNull { it.name.isNotEmpty() }?.name?.replace(NAME_ORDINAL, "") ?: key

    val country: String get() = servers.firstOrNull { it.country.isNotEmpty() }?.country.orEmpty()
}

/** The line under a region's name; `ui.text.regionSubtitleText` words it. */
sealed interface RegionSubtitle {
    data class Single(val id: String) : RegionSubtitle
    data class AutoOf(val count: Int) : RegionSubtitle
    data class Pinned(val id: String) : RegionSubtitle
    data class BestOf(val id: String, val count: Int) : RegionSubtitle
}

/** `eu-central-1` becomes `eu-central`; the hub's own region field is not used. */
fun regionKeyOf(serverId: String): String = serverId.replace(NODE_SUFFIX, "")

fun regionKeyOf(server: Server): String = regionKeyOf(server.id)

/** Regions and the nodes inside them keep hub order. */
fun groupRegions(servers: List<Server>): List<Region> =
    servers.groupBy { regionKeyOf(it) }.map { (key, nodes) -> Region(key, nodes) }

fun regionOfServer(regions: List<Region>, serverId: String): Region? =
    regions.firstOrNull { region -> region.servers.any { it.id == serverId } }

/** Regions keep only the nodes that can carry the transport; a region left empty drops out. */
fun regionsSupporting(transport: String, regions: List<Region>): List<Region> =
    groupRegions(regions.flatMap { it.servers }.filter { it.supports(transport) })

/** The selection always leads, since the top slot and its highlight are what mark it as picked. */
fun regionSlots(regions: List<Region>, selectedKey: String?, count: Int = REGION_SLOT_COUNT): List<Region> {
    val selected = regions.firstOrNull { it.key == selectedKey } ?: return regions.take(count)
    return (listOf(selected) + regions.filter { it.key != selectedKey }).take(count)
}

/** The pick while it is still listed, else the last connected server, else nothing. */
fun resolveSelection(visible: List<Server>, previous: String, lastServerId: String?): String {
    if (visible.any { it.id == previous }) return previous
    if (lastServerId != null && visible.any { it.id == lastServerId }) return lastServerId
    return ""
}

fun orderByRecent(regions: List<Region>, recent: List<String>): List<Region> {
    val ranked = recent.mapNotNull { key -> regions.firstOrNull { it.key == key } }
    val rankedKeys = ranked.map { it.key }.toSet()
    return ranked + regions.filter { it.key !in rankedKeys }
}

fun promoteRecent(recent: List<String>, key: String, limit: Int = MAX_RECENT_REGIONS): List<String> =
    (listOf(key) + recent.filter { it != key }).take(limit)

/** The chosen node, its siblings by load, then every later region in hub order, wrapping round. */
fun retryOrder(fromId: String, servers: List<Server>): List<String> {
    val regions = groupRegions(servers)
    val index = regions.indexOfFirst { region -> region.servers.any { it.id == fromId } }
    if (index < 0) return regions.flatMap { byLoad(it.servers) }.map { it.id }
    val siblings = byLoad(regions[index].servers).map { it.id }.filter { it != fromId }
    val later = regions.drop(index + 1) + regions.take(index)
    return listOf(fromId) + siblings + later.flatMap { byLoad(it.servers) }.map { it.id }
}

/** Ids to try for a start on chosenId: it, its region's siblings, then one node per other region. */
fun startCandidates(chosenId: String, servers: List<Server>, limit: Int = CANDIDATE_LIMIT): List<String> {
    val home = regionKeyOf(chosenId)
    val order = retryOrder(chosenId, servers).filter { it != chosenId }
    val siblings = order.filter { regionKeyOf(it) == home }
    val firstPerRegion = order.filter { regionKeyOf(it) != home }.distinctBy { regionKeyOf(it) }
    return (listOf(chosenId) + siblings + firstPerRegion).take(limit)
}

fun regionSubtitle(region: Region, selectedId: String?, pinnedId: String?): RegionSubtitle {
    if (region.servers.isEmpty()) return RegionSubtitle.Single(region.key)
    val count = region.servers.size
    val current = region.servers.firstOrNull { it.id == selectedId }
        ?: return if (count > 1) RegionSubtitle.AutoOf(count) else RegionSubtitle.Single(region.best.id)
    if (current.id == pinnedId) return RegionSubtitle.Pinned(current.id)
    return if (count > 1) RegionSubtitle.BestOf(current.id, count) else RegionSubtitle.Single(current.id)
}

/** Uniform over the light nodes, or over all when none is light, so new users never herd onto one. */
fun pickRandomServer(servers: List<Server>, random: Random = Random.Default): Server? {
    if (servers.isEmpty()) return null
    val light = servers.filter { server -> server.load?.let { it <= PREFERRED_MAX_LOAD } == true }
    val pool = light.ifEmpty { servers }
    return pool[random.nextInt(pool.size)]
}

private fun loadOf(server: Server): Int = server.load ?: 100

// sortedBy is stable, so nodes reporting the same load keep their hub order.
private fun byLoad(nodes: List<Server>): List<Server> = nodes.sortedBy { loadOf(it) }
