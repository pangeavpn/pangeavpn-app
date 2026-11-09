package org.pangeavpn.app.ui.regions

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.KeyboardArrowDown
import androidx.compose.material.icons.rounded.KeyboardArrowUp
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.key
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.Region
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.regionSubtitle
import org.pangeavpn.core.servers.NodeEntry
import org.pangeavpn.core.servers.RegionPicker
import org.pangeavpn.core.servers.RowRole
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.LoadMeter
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.PillBadge
import org.pangeavpn.ui.components.ServerRow
import org.pangeavpn.ui.text.nodeBadgeText
import org.pangeavpn.ui.text.regionSubtitleText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.reducedMotion

private val NodeShape = RoundedCornerShape(10.dp)

/** One region; with two or more nodes a chevron discloses them, and tapping a node pins it. */
@Composable
internal fun RegionRow(
    region: Region,
    selectedId: String?,
    pinnedId: String?,
    role: RowRole,
    expanded: Boolean,
    enabled: Boolean,
    onSelect: () -> Unit,
    onToggleNodes: () -> Unit,
    onSelectNode: (Server) -> Unit,
) {
    val nodes = RegionPicker.nodes(region, pinnedId)
    val holds = RegionPicker.holdsSelection(region, selectedId)
    val reduced = reducedMotion()

    Column {
        ServerRow(
            name = region.name,
            country = region.country,
            subtitle = regionSubtitleText(regionSubtitle(region, selectedId, pinnedId)),
            load = RegionPicker.node(region, selectedId, pinnedId).load,
            selected = holds,
            onClick = onSelect,
            modifier = Modifier.semantics { selected = holds },
            enabled = enabled && role != RowRole.LOCKED_ENTRY,
            badge = if (role == RowRole.PLAIN) null else stringResource(R.string.regions_entry_badge),
            // A row without a chevron keeps its slot, so every load meter sits in one column.
            trailing = {
                if (nodes.isEmpty()) {
                    Spacer(modifier = Modifier.size(48.dp))
                } else {
                    IconAction(
                        icon = if (expanded) Icons.Rounded.KeyboardArrowUp else Icons.Rounded.KeyboardArrowDown,
                        label = stringResource(
                            if (expanded) R.string.regions_hide_servers else R.string.regions_show_servers,
                            region.name,
                        ),
                        onClick = onToggleNodes,
                    )
                }
            },
        )
        AnimatedVisibility(
            visible = expanded && nodes.isNotEmpty(),
            enter = fadeIn(animationSpec = Motion.quick()) + expandVertically(animationSpec = Motion.layout(reduced)),
            exit = fadeOut(animationSpec = Motion.quick()) + shrinkVertically(animationSpec = Motion.layout(reduced)),
        ) {
            PangeaCard(
                padding = PaddingValues(horizontal = 6.dp, vertical = 4.dp),
                modifier = Modifier.padding(start = 12.dp, top = 4.dp),
            ) {
                nodes.forEach { entry ->
                    key(entry.server.id) {
                        NodeRow(entry = entry, enabled = enabled, onClick = { onSelectNode(entry.server) })
                    }
                }
            }
        }
    }
}

@Composable
private fun NodeRow(entry: NodeEntry, enabled: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 48.dp)
            .alpha(if (enabled) 1f else 0.55f)
            .clip(NodeShape)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(9.dp),
    ) {
        Row(
            modifier = Modifier.weight(1f),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            Text(
                text = entry.server.id,
                modifier = Modifier.weight(1f, fill = false),
                color = LocalTextSecondary.current,
                fontFamily = FontFamily.Monospace,
                fontSize = 12.sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                style = LocalTextStyle.current.copy(textDirection = TextDirection.Ltr),
            )
            entry.badge?.let { PillBadge(text = nodeBadgeText(it), outlined = true) }
        }
        entry.server.load?.let { LoadMeter(load = it) }
    }
}
