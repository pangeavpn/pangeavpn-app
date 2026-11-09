package org.pangeavpn.app.ui.main

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.MutableState
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.connection.SlotBudget
import org.pangeavpn.core.connection.SlotMetrics
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.ServersRepository
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.regionSubtitle
import org.pangeavpn.core.servers.RegionPicker
import org.pangeavpn.core.session.LoginErrorKind
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.AllRegionsRow
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.PangeaCardShape
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.ServerRow
import org.pangeavpn.ui.text.generalErrorText
import org.pangeavpn.ui.text.regionSubtitleText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.reducedMotion

internal fun Density.heightDp(size: IntSize): Float = size.height.toDp().value

/** The region rows the screen has room for, the door to the rest and the rotate button. */
@Composable
internal fun RegionSlots(
    rows: Int,
    metrics: MutableState<SlotMetrics>,
    onOpenRegions: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val servers by ServersRepository.state.collectAsState()
    val connection by ConnectionRepository.state.collectAsState()
    val canRotate by ConnectionRepository.canRotate.collectAsState()
    val settings by SettingsRepository.settings.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val density = LocalDensity.current

    val transport = settings.preferredTransport
    val slots = servers.slots(transport, rows)
    val total = servers.regionsFor(transport).size
    val selectedKey = servers.selectedRegionKey
    val selectedId = servers.selected?.id
    val pinnedId = servers.pinnedServerId
    // A pick is ignored while a start or switch is under way, so the rows say so too.
    val working = connection.inFlight

    Column(
        modifier = modifier
            .fillMaxWidth()
            .animateContentSize(Motion.layout(reducedMotion())),
        verticalArrangement = Arrangement.spacedBy(SlotBudget.SPACING.dp),
    ) {
        SectionLabel(
            text = stringResource(if (prefs.multihopEnabled) R.string.main_exit_region else R.string.hero_region),
            modifier = Modifier
                .semantics { heading() }
                .onSizeChanged { metrics.value = metrics.value.copy(label = density.heightDp(it)) },
        )
        if (slots.isEmpty()) {
            EmptyRegions(loading = servers.loading, error = servers.error)
        } else {
            slots.forEachIndexed { index, region ->
                key(region.key) {
                    ServerRow(
                        name = region.name,
                        country = region.country,
                        subtitle = regionSubtitleText(regionSubtitle(region, selectedId, pinnedId)),
                        load = RegionPicker.node(region, selectedId, pinnedId).load,
                        selected = region.key == selectedKey,
                        onClick = { ConnectionRepository.activate(region) },
                        modifier = if (index == 0) {
                            Modifier.onSizeChanged { metrics.value = metrics.value.copy(row = density.heightDp(it)) }
                        } else {
                            Modifier
                        },
                        enabled = !working,
                    )
                }
            }
        }
        AllRegionsRow(
            moreCount = (total - slots.size).coerceAtLeast(0),
            canRotate = canRotate,
            onOpen = onOpenRegions,
            onRotate = ConnectionRepository::rotate,
            modifier = Modifier.onSizeChanged { metrics.value = metrics.value.copy(door = density.heightDp(it)) },
        )
    }
}

@Composable
private fun EmptyRegions(loading: Boolean, error: LoginErrorKind?) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = 54.dp)
            .clip(PangeaCardShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(1.dp, MaterialTheme.colorScheme.outlineVariant, PangeaCardShape)
            .padding(start = 14.dp, end = 4.dp, top = 3.dp, bottom = 3.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (loading) {
            Box(modifier = Modifier.weight(1f).heightIn(min = 48.dp), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(
                    modifier = Modifier.size(22.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    strokeWidth = 2.dp,
                )
            }
        } else {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    text = stringResource(R.string.hero_no_servers),
                    color = LocalTextSecondary.current,
                    fontSize = 15.sp,
                    lineHeight = 20.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                if (error != null) {
                    Text(
                        text = generalErrorText(error),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        fontSize = 12.sp,
                        lineHeight = 16.sp,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }
            IconAction(
                icon = Icons.Rounded.Refresh,
                label = stringResource(R.string.region_refresh),
                onClick = ServersRepository::refresh,
                tint = LocalTextSecondary.current,
            )
        }
    }
}
