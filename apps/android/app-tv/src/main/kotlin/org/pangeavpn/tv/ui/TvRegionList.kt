package org.pangeavpn.tv.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.Login
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.Prefs
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.ServersRepository
import org.pangeavpn.core.data.ServersState
import org.pangeavpn.core.regionSubtitle
import org.pangeavpn.core.servers.EntryChip
import org.pangeavpn.core.servers.EntryPanel
import org.pangeavpn.core.servers.Multihop
import org.pangeavpn.core.servers.PickerEmpty
import org.pangeavpn.core.servers.RegionPicker
import org.pangeavpn.core.servers.RowRole
import org.pangeavpn.core.session.LoginErrorKind
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ChoiceRow
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PangeaDialog
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.ServerRow
import org.pangeavpn.ui.components.SettingsNavRow
import org.pangeavpn.ui.components.SwitchRow
import org.pangeavpn.ui.components.flagEmoji
import org.pangeavpn.ui.text.entryHintText
import org.pangeavpn.ui.text.generalErrorText
import org.pangeavpn.ui.text.pickerEmptyText
import org.pangeavpn.ui.text.regionSubtitleText
import org.pangeavpn.ui.theme.LocalTextSecondary

/** The region picker kept on screen: multihop on top when a server can relay, then every region in hub
 *  order, so the row under the remote's focus never moves when a pick reorders the recents. */
@Composable
fun TvRegionList(
    servers: ServersState,
    transport: String,
    prefs: Prefs,
    working: Boolean,
    modifier: Modifier = Modifier,
) {
    val connection by ConnectionRepository.state.collectAsState()
    val regions = servers.regionsFor(transport)
    val empty = RegionPicker.empty(regions, servers.regions)
    val selectedId = servers.selected?.id
    val pinnedId = servers.pinnedServerId
    val exitId = selectedId.orEmpty()
    val chosenId = prefs.multihopEntryId
    val multihop = Multihop.available(servers.regions)
    val multihopOn = multihop && prefs.multihopEnabled
    val entry = if (multihopOn) Multihop.resolveEntry(regions.flatMap { it.servers }, exitId, chosenId) else null
    val listError = servers.error.takeIf { empty == null }

    val listState = rememberLazyListState()
    val firstRegion = remember { FocusRequester() }
    var picking by remember { mutableStateOf(false) }
    val leading = (if (multihop) 1 else 0) + (if (multihopOn) 1 else 0) + 1 + (if (listError != null) 1 else 0)

    // Opens on the selection, wherever hub order puts it.
    LaunchedEffect(regions.isNotEmpty()) {
        val index = regions.indexOfFirst { RegionPicker.holdsSelection(it, selectedId) }
        if (index > 0) listState.scrollToItem(leading + index)
    }

    // Connect with nothing to connect to asks for a pick; the list is already here, so it takes the focus.
    LaunchedEffect(connection.showServerSheet) {
        if (!connection.showServerSheet) return@LaunchedEffect
        ConnectionRepository.setServerSheetShown(false)
        if (regions.isEmpty()) return@LaunchedEffect
        listState.scrollToItem(leading)
        withFrameNanos {}
        firstRegion.tryFocus()
    }

    LazyColumn(
        state = listState,
        modifier = modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(9.dp),
        contentPadding = PaddingValues(vertical = 4.dp),
    ) {
        if (multihop) {
            item(key = "multihop") {
                SwitchRow(
                    title = stringResource(R.string.regions_multihop),
                    checked = prefs.multihopEnabled,
                    onCheckedChange = { ConnectionRepository.setMultihop(it) },
                    hint = stringResource(R.string.regions_multihop_hint),
                )
            }
        }
        if (multihopOn) {
            item(key = "entry") {
                EntryRow(panel = Multihop.entryPanel(regions, exitId, chosenId), onOpen = { picking = true })
            }
        }
        item(key = "heading") {
            SectionLabel(
                text = stringResource(if (prefs.multihopEnabled) R.string.main_exit_region else R.string.hero_region),
                modifier = Modifier.padding(start = 4.dp, top = 4.dp),
            )
        }
        if (empty != null) {
            item(key = "empty") {
                EmptyRegions(empty = empty, loading = servers.loading, error = servers.error)
            }
        } else {
            if (listError != null) {
                item(key = "error") {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        InfoNote(text = generalErrorText(listError), tone = NoteTone.ERROR)
                        GhostButton(
                            label = stringResource(R.string.restore_failed_retry),
                            onClick = { ServersRepository.refresh() },
                            loading = servers.loading,
                            compact = true,
                        )
                    }
                }
            }
            itemsIndexed(regions, key = { _, region -> region.key }) { index, region ->
                val role = if (multihopOn) Multihop.role(region, entry, chosenId, selectedId) else RowRole.PLAIN
                val locked = role == RowRole.LOCKED_ENTRY
                // Dimmed, never disabled: a disabled row would drop the remote's focus mid-switch.
                ServerRow(
                    name = region.name,
                    country = region.country,
                    subtitle = regionSubtitleText(regionSubtitle(region, selectedId, pinnedId)),
                    load = RegionPicker.node(region, selectedId, pinnedId).load,
                    selected = RegionPicker.holdsSelection(region, selectedId),
                    onClick = { if (!working && !locked) ConnectionRepository.activate(region) },
                    modifier = Modifier
                        .alpha(if (working || locked) 0.6f else 1f)
                        .then(if (index == 0) Modifier.focusRequester(firstRegion) else Modifier),
                    badge = if (role == RowRole.PLAIN) null else stringResource(R.string.regions_entry_badge),
                )
            }
        }
    }

    LaunchedEffect(multihopOn) { if (!multihopOn) picking = false }
    if (picking && multihopOn) {
        EntryDialog(
            panel = Multihop.entryPanel(regions, exitId, chosenId),
            onChoose = { serverId ->
                picking = false
                ConnectionRepository.setMultihopEntry(serverId)
            },
            onDismiss = { picking = false },
        )
    }
}

@Composable
private fun chipLabel(chip: EntryChip): String =
    if (chip.regionKey == null) stringResource(R.string.regions_entry_auto) else "${flagEmoji(chip.country)}  ${chip.name}"

// The remote has no chips to tap through, so the entry is one row that opens the choice.
@Composable
private fun EntryRow(panel: EntryPanel, onOpen: () -> Unit) {
    val chosen = panel.chips.firstOrNull { it.selected } ?: panel.chips.first()
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        SettingsNavRow(
            icon = Icons.AutoMirrored.Rounded.Login,
            title = stringResource(R.string.regions_entry),
            onClick = onOpen,
            summary = chipLabel(chosen),
        )
        entryHintText(panel.hint)?.let { hint ->
            Text(
                text = hint,
                modifier = Modifier.padding(horizontal = 4.dp),
                color = LocalTextSecondary.current,
                fontFamily = FontFamily.Monospace,
                fontSize = 11.sp,
                lineHeight = 15.sp,
            )
        }
    }
}

@Composable
private fun EntryDialog(panel: EntryPanel, onChoose: (String?) -> Unit, onDismiss: () -> Unit) {
    val chosen = remember { FocusRequester() }
    PangeaDialog(title = stringResource(R.string.regions_entry), onDismiss = onDismiss) {
        Column(modifier = Modifier.selectableGroup(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            panel.chips.forEach { chip ->
                ChoiceRow(
                    label = chipLabel(chip),
                    selected = chip.selected,
                    onClick = { onChoose(chip.serverId) },
                    hint = if (chip.isExit) stringResource(R.string.regions_same_as_exit) else null,
                    enabled = !chip.isExit,
                    modifier = if (chip.selected) Modifier.focusRequester(chosen) else Modifier,
                )
            }
        }
        LaunchedEffect(Unit) { chosen.tryFocus() }
    }
}

@Composable
private fun EmptyRegions(empty: PickerEmpty, loading: Boolean, error: LoginErrorKind?) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 54.dp)
            .tvCard(focused = false)
            .padding(start = 14.dp, end = 4.dp, top = 4.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(modifier = Modifier.weight(1f)) {
            if (loading) {
                CircularProgressIndicator(
                    modifier = Modifier.size(22.dp).align(Alignment.Center),
                    color = MaterialTheme.colorScheme.primary,
                    strokeWidth = 2.dp,
                )
            } else {
                Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(
                        text = pickerEmptyText(empty),
                        color = LocalTextSecondary.current,
                        fontSize = 15.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                    if (error != null) {
                        Text(
                            text = generalErrorText(error),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            fontSize = 12.sp,
                            lineHeight = 16.sp,
                            maxLines = 2,
                        )
                    }
                }
            }
        }
        // Kept while loading, so a remote resting on it does not lose its place.
        IconAction(
            icon = Icons.Rounded.Refresh,
            label = stringResource(R.string.region_refresh),
            onClick = { ServersRepository.refresh() },
        )
    }
}
