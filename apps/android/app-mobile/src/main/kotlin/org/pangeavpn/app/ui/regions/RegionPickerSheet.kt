package org.pangeavpn.app.ui.regions

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Dns
import androidx.compose.material.icons.rounded.FilterList
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.Region
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.ServersRepository
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.servers.Multihop
import org.pangeavpn.core.servers.PickerEmpty
import org.pangeavpn.core.servers.RegionPicker
import org.pangeavpn.core.servers.RowRole
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.ToastHost
import org.pangeavpn.ui.text.generalErrorText
import org.pangeavpn.ui.text.pickerEmptyText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateError

/** The full region list, with multihop on top when any server can relay. Refreshes the list on open. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RegionPickerSheet(onDismiss: () -> Unit) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    val scope = rememberCoroutineScope()
    val servers by ServersRepository.state.collectAsState()
    val connection by ConnectionRepository.state.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val settings by SettingsRepository.settings.collectAsState()
    var expanded by remember { mutableStateOf(emptySet<String>()) }

    LaunchedEffect(Unit) { ServersRepository.refresh() }

    // The pick runs in the repository's own scope, so it survives the sheet sliding away.
    fun pick(activate: () -> Unit) {
        if (ConnectionRepository.state.value.inFlight) return
        activate()
        scope.launch { sheetState.hide() }.invokeOnCompletion { onDismiss() }
    }

    val visible = servers.regionsFor(settings.preferredTransport)
    val empty = RegionPicker.empty(visible, servers.regions)
    val multihopAvailable = Multihop.available(servers.regions)
    val multihopOn = prefs.multihopEnabled && multihopAvailable
    val selectedId = servers.selected?.id
    val exitId = selectedId.orEmpty()
    val chosenId = prefs.multihopEntryId
    val entry = if (multihopOn) Multihop.resolveEntry(visible.flatMap { it.servers }, exitId, chosenId) else null
    val sections = RegionPicker.sections(visible, servers.recentRegionKeys)
    val errorText = servers.error?.let { generalErrorText(it) }
    val failedOverList = empty == null && errorText != null
    val recentTitle = stringResource(R.string.regions_recent)
    val restTitle = stringResource(if (multihopOn) R.string.regions_exit_region else R.string.hero_all_regions)

    val row: @Composable (Region) -> Unit = { region ->
        RegionRow(
            region = region,
            selectedId = selectedId,
            pinnedId = servers.pinnedServerId,
            role = if (multihopOn) Multihop.role(region, entry, chosenId, selectedId) else RowRole.PLAIN,
            expanded = region.key in expanded,
            enabled = !connection.inFlight,
            onSelect = { pick { ConnectionRepository.activate(region) } },
            onToggleNodes = { expanded = if (region.key in expanded) expanded - region.key else expanded + region.key },
            onSelectNode = { server -> pick { ConnectionRepository.activate(server) } },
        )
    }

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        // The sheet draws edge to edge; this keeps a full-height list clear of the status bar.
        modifier = Modifier.windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Top)),
        sheetState = sheetState,
        sheetMaxWidth = 640.dp,
        containerColor = MaterialTheme.colorScheme.background,
        contentWindowInsets = { WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Bottom) },
    ) {
        Box(modifier = Modifier.fillMaxWidth()) {
            Column(modifier = Modifier.fillMaxWidth()) {
                Text(
                    text = stringResource(R.string.regions_title),
                    modifier = Modifier
                        .padding(start = 20.dp, end = 20.dp, bottom = 4.dp)
                        .semantics { heading() },
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 22.sp,
                    fontWeight = FontWeight.Light,
                    letterSpacing = (-0.4).sp,
                )
                LazyColumn(
                    modifier = Modifier.fillMaxWidth(),
                    contentPadding = PaddingValues(start = 16.dp, top = 8.dp, end = 16.dp, bottom = 32.dp),
                    verticalArrangement = Arrangement.spacedBy(9.dp),
                ) {
                    if (empty == null && errorText != null) {
                        item(key = "error") {
                            RefreshFailed(errorText, servers.loading, ServersRepository::refresh)
                        }
                    }
                    if (multihopAvailable) {
                        item(key = "multihop") {
                            MultihopPanel(
                                enabled = prefs.multihopEnabled,
                                panel = Multihop.entryPanel(visible, exitId, chosenId),
                                onToggle = ConnectionRepository::setMultihop,
                                onChoose = ConnectionRepository::setMultihopEntry,
                            )
                        }
                    }
                    if (empty != null) {
                        item(key = "empty") {
                            EmptyRegionList(empty, errorText, servers.loading, ServersRepository::refresh)
                        }
                    } else {
                        val lead = failedOverList || multihopAvailable
                        section("recent", recentTitle, sections.recent, gap = lead, row = row)
                        section("rest", restTitle, sections.rest, gap = lead || sections.recent.isNotEmpty(), row = row)
                    }
                }
            }
            ToastHost()
        }
    }
}

private fun LazyListScope.section(
    id: String,
    title: String,
    regions: List<Region>,
    gap: Boolean,
    row: @Composable (Region) -> Unit,
) {
    if (regions.isEmpty()) return
    item(key = "section:$id") {
        SectionLabel(
            text = title,
            modifier = Modifier
                .padding(start = 4.dp, top = if (gap) 7.dp else 0.dp)
                .semantics { heading() },
        )
    }
    items(regions, key = { "region:${it.key}" }) { region -> row(region) }
}

@Composable
private fun RefreshFailed(message: String, loading: Boolean, onRetry: () -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        InfoNote(text = message, tone = NoteTone.ERROR)
        GhostButton(
            label = stringResource(R.string.restore_failed_retry),
            onClick = onRetry,
            loading = loading,
            compact = true,
        )
    }
}

@Composable
private fun EmptyRegionList(empty: PickerEmpty, error: String?, loading: Boolean, onRefresh: () -> Unit) {
    PangeaCard(padding = PaddingValues(horizontal = 20.dp, vertical = 28.dp)) {
        Column(
            modifier = Modifier.fillMaxWidth(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Icon(
                imageVector = if (empty == PickerEmpty.NO_SERVERS) Icons.Rounded.Dns else Icons.Rounded.FilterList,
                contentDescription = null,
                modifier = Modifier.size(28.dp),
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = pickerEmptyText(empty),
                color = LocalTextSecondary.current,
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                textAlign = TextAlign.Center,
            )
            if (!error.isNullOrEmpty()) {
                Text(
                    text = error,
                    color = StateError,
                    fontSize = 12.sp,
                    lineHeight = 16.sp,
                    textAlign = TextAlign.Center,
                )
            }
            GhostButton(
                label = stringResource(R.string.region_refresh),
                onClick = onRefresh,
                loading = loading,
                compact = true,
                modifier = Modifier.padding(top = 6.dp),
            )
        }
    }
}
