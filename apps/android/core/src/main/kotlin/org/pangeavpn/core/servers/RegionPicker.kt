package org.pangeavpn.core.servers

import org.pangeavpn.core.Region
import org.pangeavpn.core.model.Server

enum class PickerEmpty { NO_SERVERS, NONE_FOR_TRANSPORT }

data class PickerSections(val recent: List<Region>, val rest: List<Region>)

enum class NodeBadge { PINNED, BEST }

data class NodeEntry(val server: Server, val badge: NodeBadge?)

/** The region picker's decisions, kept free of screens and repositories. */
object RegionPicker {
    fun sections(regions: List<Region>, recentKeys: List<String>): PickerSections {
        val recent = recentKeys.distinct().mapNotNull { key -> regions.firstOrNull { it.key == key } }
        val recentKeySet = recent.map { it.key }.toSet()
        return PickerSections(recent, regions.filter { it.key !in recentKeySet })
    }

    fun empty(visible: List<Region>, all: List<Region>): PickerEmpty? = when {
        visible.isNotEmpty() -> null
        all.isEmpty() -> PickerEmpty.NO_SERVERS
        else -> PickerEmpty.NONE_FOR_TRANSPORT
    }

    fun holdsSelection(region: Region, selectedId: String?): Boolean =
        selectedId != null && region.servers.any { it.id == selectedId }

    /** The node a region's row speaks for: the pin, else the node in use, else the lightest. */
    fun node(region: Region, selectedId: String?, pinnedId: String?): Server =
        region.servers.firstOrNull { it.id == pinnedId }
            ?: region.servers.firstOrNull { it.id == selectedId }
            ?: region.best

    /** Node rows are listed only for regions with two or more; the pin's badge beats the best's. */
    fun nodes(region: Region, pinnedId: String?): List<NodeEntry> {
        if (region.servers.size < 2) return emptyList()
        val bestId = region.best.id
        return region.servers.map { server ->
            val badge = when (server.id) {
                pinnedId -> NodeBadge.PINNED
                bestId -> NodeBadge.BEST
                else -> null
            }
            NodeEntry(server, badge)
        }
    }
}
