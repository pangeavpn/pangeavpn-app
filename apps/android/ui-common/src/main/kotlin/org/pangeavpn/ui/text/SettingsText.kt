package org.pangeavpn.ui.text

import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import org.pangeavpn.core.data.SettingsError
import org.pangeavpn.core.diagnostics.ReportFailure
import org.pangeavpn.core.model.HubStatus
import org.pangeavpn.core.model.HubTestResult
import org.pangeavpn.core.settings.AppLanguages
import org.pangeavpn.core.settings.DnsChoice
import org.pangeavpn.core.settings.HubMethodRules
import org.pangeavpn.core.settings.HubStatusLine
import org.pangeavpn.core.settings.HubTestOutcome
import org.pangeavpn.core.settings.HubTestTone
import org.pangeavpn.core.settings.RangesParse
import org.pangeavpn.core.settings.SettingsSummary
import org.pangeavpn.core.settings.SummaryPart
import org.pangeavpn.core.settings.TunnelMtu
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.hubMethodTitle
import org.pangeavpn.ui.components.transportChoiceLabel
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateError

// Unicode left-to-right isolate and its closing pop.
private val LRI = Char(0x2066)
private val PDI = Char(0x2069)

@Composable
fun dnsChoiceTitle(choice: DnsChoice): String = stringResource(
    when (choice) {
        DnsChoice.AUTOMATIC -> R.string.network_dns_automatic
        DnsChoice.ADGUARD -> R.string.network_dns_adguard
        DnsChoice.CLOUDFLARE -> R.string.network_dns_cloudflare
        DnsChoice.QUAD9 -> R.string.network_dns_quad9
        DnsChoice.CLOUDFLARE_FAMILY -> R.string.network_dns_cloudflare_family
        DnsChoice.GOOGLE -> R.string.network_dns_google
        DnsChoice.CUSTOM -> R.string.network_dns_custom
    },
)

@Composable
fun mtuInvalidText(): String =
    stringResource(R.string.network_mtu_invalid, TunnelMtu.range.first, TunnelMtu.range.last)

/** The note a settings page shows while [org.pangeavpn.core.data.SettingsRepository.error] is set. */
@Composable
fun settingsErrorText(error: SettingsError): String = stringResource(
    when (error) {
        SettingsError.LAST_HUB_METHOD -> R.string.settings_censorship_last_method
        SettingsError.FAILED -> R.string.network_update_failed
    },
)

/** The line under a hub method's Test button. */
@Composable
fun hubTestText(result: HubTestResult): String = when (val outcome = HubMethodRules.outcome(result)) {
    is HubTestOutcome.Works ->
        if (outcome.via.isEmpty()) stringResource(R.string.provisioning_result_ok, outcome.ms)
        else stringResource(R.string.provisioning_result_ok_detail, outcome.via, outcome.ms)
    HubTestOutcome.Failed -> stringResource(R.string.provisioning_result_fail)
    is HubTestOutcome.NothingToTry -> stringResource(
        when (outcome) {
            HubTestOutcome.NothingToTry.NO_ADDRESS -> R.string.provisioning_result_no_address
            HubTestOutcome.NothingToTry.NO_REALITY_CREDENTIALS -> R.string.provisioning_result_no_reality_credentials
            HubTestOutcome.NothingToTry.NO_SHADOWSOCKS_CREDENTIALS -> R.string.provisioning_result_no_credentials
            HubTestOutcome.NothingToTry.NO_RELAY -> R.string.provisioning_result_no_relay
            HubTestOutcome.NothingToTry.BUSY -> R.string.provisioning_result_busy
        },
    )
}

/** Green when it worked, amber when there was nothing to try, red when the network failed it. */
fun hubTestColor(result: HubTestResult): Color = when (HubMethodRules.outcome(result).tone) {
    HubTestTone.OK -> StateConnected
    HubTestTone.UNAVAILABLE -> StateConnecting
    HubTestTone.FAILED -> StateError
}

/** The "In use" line above the hub methods. */
@Composable
fun hubStatusText(status: HubStatus): String = when (val line = HubMethodRules.statusLine(status)) {
    HubStatusLine.None -> stringResource(R.string.provisioning_active_none)
    is HubStatusLine.Using ->
        if (line.detail.isEmpty()) stringResource(R.string.provisioning_active_using, hubMethodTitle(line.method))
        else stringResource(R.string.provisioning_active_detail, hubMethodTitle(line.method), line.detail)
}

/** One sentence per kind of problem, naming the entries it applies to, as desktop words it. */
@Composable
fun rangesErrorText(refused: RangesParse.Refused): String {
    val sentences = mutableListOf<String>()
    if (refused.notIPv4.isNotEmpty()) {
        sentences += stringResource(R.string.split_tunnel_error_not_ipv4, isolated(refused.notIPv4))
    }
    if (refused.prefixTooShort.isNotEmpty()) {
        sentences += stringResource(R.string.split_tunnel_error_too_broad, isolated(refused.prefixTooShort))
    }
    if (refused.tooMany) sentences += stringResource(R.string.split_tunnel_error_too_many)
    return sentences.joinToString(" ")
}

// Isolated left to right, so an address stays intact inside Arabic or Persian text.
private fun isolated(tokens: List<String>): String = tokens.joinToString(", ") { "$LRI$it$PDI" }

@Composable
fun reportFailureText(failure: ReportFailure): String = stringResource(
    when (failure) {
        ReportFailure.REJECTED -> R.string.support_rejected_body
        ReportFailure.UNREACHABLE -> R.string.support_failed_body
    },
)

/** The line under a settings home row: the parts joined by a middle dot, or Off when there are none. */
@Composable
fun summaryText(parts: List<SummaryPart>): String {
    val shown = parts.map { summaryPartText(it) }.filter { it.isNotEmpty() }
    if (shown.isEmpty()) return stringResource(R.string.settings_summary_off)
    return shown.joinToString(SettingsSummary.SEPARATOR)
}

@Composable
private fun summaryPartText(part: SummaryPart): String = when (part) {
    is SummaryPart.Transport -> transportChoiceLabel(part.choice)
    is SummaryPart.HubMethod -> hubMethodTitle(part.method)
    is SummaryPart.Mtu -> stringResource(R.string.network_summary_mtu, part.value)
    is SummaryPart.Dns -> dnsChoiceTitle(part.choice)
    is SummaryPart.ExcludedApps ->
        pluralStringResource(R.plurals.split_tunnel_summary_apps, part.count, part.count)
    is SummaryPart.ExcludedRanges ->
        pluralStringResource(R.plurals.split_tunnel_summary_ranges, part.count, part.count)
    is SummaryPart.Literal -> part.text
    is SummaryPart.Flag -> stringResource(
        when (part) {
            SummaryPart.Flag.ALLOW_LAN -> R.string.settings_network_allowlan_title
            SummaryPart.Flag.POST_QUANTUM -> R.string.network_summary_pq
            SummaryPart.Flag.KILL_SWITCH_STRICT -> R.string.kill_switch_summary_strict
            SummaryPart.Flag.KILL_SWITCH_ALWAYS_ON -> R.string.kill_switch_always_on
            SummaryPart.Flag.NOTHING_EXCLUDED -> R.string.split_tunnel_summary_empty
            SummaryPart.Flag.AUTO_CONNECT -> R.string.startup_auto_connect_title
            SummaryPart.Flag.LOCKDOWN -> R.string.startup_lockdown_title
            SummaryPart.Flag.CONNECTION_ALERTS -> R.string.notifications_alerts_title
            SummaryPart.Flag.PANIC_ENABLED -> R.string.panic_summary_on
            SummaryPart.Flag.DEVELOPER_ON -> R.string.developer_summary_on
        },
    )
}

/** The tag of the language the app is showing, whether picked in the app or followed from the system. */
@Composable
fun displayedLanguageTag(): String {
    val supported = AppLanguages.supported.map { it.code }.toTypedArray()
    return LocalConfiguration.current.locales.getFirstMatch(supported)?.language ?: supported[0]
}
