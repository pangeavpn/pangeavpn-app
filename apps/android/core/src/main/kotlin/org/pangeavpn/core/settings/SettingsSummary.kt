package org.pangeavpn.core.settings

import org.pangeavpn.core.Prefs
import org.pangeavpn.core.model.AppSettings

/** One piece of the line under a settings home row. */
sealed interface SummaryPart {
    data class Transport(val choice: String) : SummaryPart
    data class HubMethod(val method: String) : SummaryPart
    data class Mtu(val value: Int) : SummaryPart
    data class Dns(val choice: DnsChoice) : SummaryPart
    data class ExcludedApps(val count: Int) : SummaryPart
    data class ExcludedRanges(val count: Int) : SummaryPart

    /** Shown as it is: a language's own name, a version. */
    data class Literal(val text: String) : SummaryPart

    enum class Flag : SummaryPart {
        ALLOW_LAN, POST_QUANTUM, KILL_SWITCH_STRICT, KILL_SWITCH_ALWAYS_ON, NOTHING_EXCLUDED,
        AUTO_CONNECT, LOCKDOWN, CONNECTION_ALERTS, PANIC_ENABLED, DEVELOPER_ON,
    }
}

/** The one-line value under each settings home row; an empty list reads as Off. */
object SettingsSummary {
    const val SEPARATOR = " · "

    fun transport(settings: AppSettings): List<SummaryPart> =
        listOf(SummaryPart.Transport(TransportChoices.selected(settings.preferredTransport)))

    fun provisioning(settings: AppSettings): List<SummaryPart> =
        HubMethodRules.enabled(settings.hubMethods).map { SummaryPart.HubMethod(it) }

    fun network(settings: AppSettings): List<SummaryPart> = buildList {
        add(SummaryPart.Mtu(settings.mtu))
        val dns = DnsPresets.choice(settings.customDns)
        if (dns != DnsChoice.AUTOMATIC) add(SummaryPart.Dns(dns))
        if (settings.allowLan) add(SummaryPart.Flag.ALLOW_LAN)
        if (settings.postQuantum) add(SummaryPart.Flag.POST_QUANTUM)
    }

    /** [strict] is the system's block-without-VPN setting, when the caller can tell. */
    fun killSwitch(strict: Boolean): List<SummaryPart> =
        listOf(if (strict) SummaryPart.Flag.KILL_SWITCH_STRICT else SummaryPart.Flag.KILL_SWITCH_ALWAYS_ON)

    /** Android excludes apps as well as ranges, so the line counts both. */
    fun splitTunnel(prefs: Prefs, excludedApps: Int): List<SummaryPart> {
        if (!prefs.splitTunnelEnabled) return emptyList()
        val ranges = prefs.splitTunnelRanges.size
        if (excludedApps == 0 && ranges == 0) return listOf(SummaryPart.Flag.NOTHING_EXCLUDED)
        return buildList {
            if (excludedApps > 0) add(SummaryPart.ExcludedApps(excludedApps))
            if (ranges > 0) add(SummaryPart.ExcludedRanges(ranges))
        }
    }

    fun startup(prefs: Prefs): List<SummaryPart> = buildList {
        if (prefs.autoConnect || prefs.lockdown) add(SummaryPart.Flag.AUTO_CONNECT)
        if (prefs.lockdown) add(SummaryPart.Flag.LOCKDOWN)
    }

    fun notifications(prefs: Prefs): List<SummaryPart> =
        if (prefs.connectionAlerts) listOf(SummaryPart.Flag.CONNECTION_ALERTS) else emptyList()

    fun panic(prefs: Prefs): List<SummaryPart> =
        if (prefs.panicWipe) listOf(SummaryPart.Flag.PANIC_ENABLED) else emptyList()

    fun developer(prefs: Prefs): List<SummaryPart> =
        if (prefs.developerMode) listOf(SummaryPart.Flag.DEVELOPER_ON) else emptyList()

    /** [tag] is the language the app is showing, not the stored choice, which may be "follow the system". */
    fun language(tag: String?): List<SummaryPart> = listOf(SummaryPart.Literal(AppLanguages.forTag(tag).name))

    fun update(version: String): List<SummaryPart> = listOf(SummaryPart.Literal("v$version"))
}
