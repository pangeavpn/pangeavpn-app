package org.pangeavpn.core.servers

import org.pangeavpn.core.Region
import org.pangeavpn.core.groupRegions
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.regionKeyOf
import org.pangeavpn.core.regionOfServer

/** A hand-picked entry cannot also be the exit, so its row is locked; an automatic one just moves. */
enum class RowRole { PLAIN, ENTRY, LOCKED_ENTRY }

data class EntryChip(
    // Null is the Auto chip.
    val regionKey: String?,
    val name: String,
    val country: String,
    // What choosing the chip stores: the region's best relay, or null for Auto.
    val serverId: String?,
    val selected: Boolean,
    // The exit's own region: shown, but disabled.
    val isExit: Boolean,
)

sealed interface EntryHint {
    data object Hidden : EntryHint
    data class AutoPicks(val regionName: String) : EntryHint
    data object NoEntries : EntryHint
}

data class EntryPanel(val chips: List<EntryChip>, val hint: EntryHint)

data class RouteNode(val name: String, val country: String)

object Multihop {
    fun available(regions: List<Region>): Boolean =
        regions.any { region -> region.servers.any { it.multihop } }

    /** Relays outside the exit's region, lightest first and in hub order on ties. */
    fun entryCandidates(servers: List<Server>, exitId: String): List<Server> {
        val exitRegion = regionKeyOf(exitId)
        return servers.filter { it.multihop && regionKeyOf(it) != exitRegion }.sortedBy { it.load ?: 100 }
    }

    /** The entry a route will really use: the choice while it still qualifies, else the lightest. */
    fun resolveEntry(servers: List<Server>, exitId: String, chosenId: String?): Server? {
        val candidates = entryCandidates(servers, exitId)
        return candidates.firstOrNull { it.id == chosenId } ?: candidates.firstOrNull()
    }

    fun entryPanel(visible: List<Region>, exitId: String, chosenId: String?): EntryPanel {
        val servers = visible.flatMap { it.servers }
        val candidates = entryCandidates(servers, exitId)
        val chosen = candidates.firstOrNull { it.id == chosenId }
        val chosenRegion = chosen?.let { regionKeyOf(it) }
        val exitRegion = exitId.takeIf { it.isNotEmpty() }?.let { regionKeyOf(it) }
        val regionChips = groupRegions(servers.filter { it.multihop }).map { region ->
            EntryChip(
                regionKey = region.key,
                name = region.name,
                country = region.country,
                serverId = region.best.id,
                selected = region.key == chosenRegion,
                isExit = region.key == exitRegion,
            )
        }
        val auto = EntryChip(null, name = "", country = "", serverId = null, selected = chosen == null, isExit = false)
        return EntryPanel(listOf(auto) + regionChips, hint(chosen, candidates.firstOrNull(), visible))
    }

    /** The exit's own row is never marked as the entry. */
    fun role(region: Region, entry: Server?, chosenId: String?, selectedId: String?): RowRole {
        if (RegionPicker.holdsSelection(region, selectedId)) return RowRole.PLAIN
        if (entry == null || region.servers.none { it.id == entry.id }) return RowRole.PLAIN
        return if (entry.id == chosenId) RowRole.LOCKED_ENTRY else RowRole.ENTRY
    }

    /** The tunnel's own name for a node the list no longer carries, then its id as a last resort. */
    fun routeNode(id: String, fallbackName: String, regions: List<Region>): RouteNode {
        val server = regions.firstNotNullOfOrNull { region -> region.servers.firstOrNull { it.id == id } }
            ?: return RouteNode(fallbackName.ifEmpty { id }, country = "")
        return RouteNode(server.name.ifEmpty { server.id }, server.country)
    }

    private fun hint(chosen: Server?, auto: Server?, visible: List<Region>): EntryHint = when {
        chosen != null -> EntryHint.Hidden
        auto == null -> EntryHint.NoEntries
        else -> EntryHint.AutoPicks(regionOfServer(visible, auto.id)?.name ?: auto.name)
    }
}
