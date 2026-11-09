package org.pangeavpn.ui.text

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import org.pangeavpn.core.RegionSubtitle
import org.pangeavpn.core.connection.ConnectProgress
import org.pangeavpn.core.connection.HeroDetailLine
import org.pangeavpn.core.connection.MainAction
import org.pangeavpn.core.servers.EntryHint
import org.pangeavpn.core.servers.NodeBadge
import org.pangeavpn.core.servers.PickerEmpty
import org.pangeavpn.ui.R

@Composable
fun regionSubtitleText(subtitle: RegionSubtitle): String = when (subtitle) {
    is RegionSubtitle.Single -> subtitle.id
    is RegionSubtitle.AutoOf ->
        pluralStringResource(R.plurals.region_auto_of, subtitle.count, subtitle.count.toString())
    is RegionSubtitle.Pinned -> stringResource(R.string.region_pinned, subtitle.id)
    is RegionSubtitle.BestOf -> stringResource(R.string.region_best_of, subtitle.id, subtitle.count.toString())
}

@Composable
fun connectProgressText(step: ConnectProgress): String = stringResource(
    when (step) {
        ConnectProgress.STILL_CONNECTING -> R.string.connect_progress_still_connecting
        ConnectProgress.TAKING_LONGER -> R.string.connect_progress_taking_longer
        ConnectProgress.STILL_WORKING -> R.string.connect_progress_still_working
    },
)

@Composable
fun heroDetailText(line: HeroDetailLine): String = when (line) {
    HeroDetailLine.Paused -> stringResource(R.string.main_paused_detail)
    is HeroDetailLine.Progress -> connectProgressText(line.step)
    is HeroDetailLine.Text -> line.text
}

/** The action bar's label; null for the two actions that draw no button. */
@Composable
fun mainActionText(action: MainAction): String? = when (action) {
    MainAction.CONNECT -> stringResource(R.string.hero_connect)
    MainAction.PROVISIONING -> stringResource(R.string.hero_provisioning)
    MainAction.STOP -> stringResource(R.string.main_stop)
    MainAction.DISCONNECT -> stringResource(R.string.hero_disconnect)
    MainAction.DISCONNECTING -> stringResource(R.string.hero_disconnecting)
    MainAction.LOCKDOWN, MainAction.HIDDEN -> null
}

@Composable
fun entryHintText(hint: EntryHint): String? = when (hint) {
    EntryHint.Hidden -> null
    is EntryHint.AutoPicks -> stringResource(R.string.regions_entry_auto_picks, hint.regionName)
    EntryHint.NoEntries -> stringResource(R.string.regions_no_entries)
}

@Composable
fun nodeBadgeText(badge: NodeBadge): String = stringResource(
    when (badge) {
        NodeBadge.PINNED -> R.string.regions_badge_pinned
        NodeBadge.BEST -> R.string.regions_badge_best
    },
)

@Composable
fun pickerEmptyText(empty: PickerEmpty): String = stringResource(
    when (empty) {
        PickerEmpty.NO_SERVERS -> R.string.hero_no_servers
        PickerEmpty.NONE_FOR_TRANSPORT -> R.string.server_picker_none_for_transport
    },
)
