package org.pangeavpn.core.tunnel

import android.app.PendingIntent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import org.pangeavpn.core.LAN_RANGES
import org.pangeavpn.core.Route
import org.pangeavpn.core.includedRoutes
import org.pangeavpn.core.model.TunnelConfig
import org.pangeavpn.core.settings.SplitTunnelRanges
import org.pangeavpn.core.tunnelRoutesV6

/** Everything that shapes a TUN, so an unchanged one is never established twice. */
internal data class TunnelSettings(
    val address: String,
    val prefixLength: Int,
    val dns: List<String>,
    val mtu: Int,
    val allowLan: Boolean,
    val excludedRanges: List<String>,
    val excludedApps: Set<String>,
)

/** iOS's TunnelSettingsBuilder, ending in a [VpnService.Builder]. */
internal object TunnelSettingsBuilder {
    private const val SESSION = "PangeaVPN"
    private const val BLACKHOLE_V6 = "fd00::1"
    private const val BLACKHOLE_PREFIX_V6 = 128
    private const val HOST_PREFIX_V4 = 32

    // Documentation addresses reach nothing, so traffic fails closed until the real config lands.
    val blockingConfig = TunnelConfig(
        address = "192.0.2.2",
        prefixLength = 32,
        dns = listOf("192.0.2.1"),
        mtu = 1280,
        serverId = "",
        serverName = "",
    )

    fun build(c: TunnelConfig, allowLan: Boolean, ranges: List<String>, apps: Set<String>): TunnelSettings {
        val settings = make(c, allowLan, ranges, apps)
        if (settings.dns.isEmpty()) throw TunnelStartException(TunnelFailure.INVALID_DNS)
        return settings
    }

    fun blocking(allowLan: Boolean, ranges: List<String>, apps: Set<String>): TunnelSettings =
        make(blockingConfig, allowLan, ranges, apps)

    private fun make(c: TunnelConfig, allowLan: Boolean, ranges: List<String>, apps: Set<String>) =
        TunnelSettings(c.address, c.prefixLength, c.dns.mapNotNull(::usableResolver), c.mtu, allowLan, ranges, apps)

    /** Null once the VPN permission is gone. The set is the excluded apps that are still installed. */
    fun establish(
        service: VpnService,
        s: TunnelSettings,
        configure: PendingIntent?,
    ): Pair<ParcelFileDescriptor, Set<String>>? {
        val builder = service.Builder()
            .setSession(SESSION)
            .addAddress(s.address, s.prefixLength)
        val carved = (if (s.allowLan) LAN_RANGES else emptyList()) + excludedRoutes(s.excludedRanges)
        includedRoutes(carved).forEach { builder.addRoute(it.address, it.prefixLength) }
        // A carved-out range could otherwise send a resolver around the tunnel.
        if (carved.isNotEmpty()) s.dns.forEach { builder.addRoute(it, HOST_PREFIX_V4) }
        builder.blackholeIpv6(s.allowLan)
        s.dns.forEach { builder.addDnsServer(it) }
        builder.setMtu(s.mtu)
        builder.setBlocking(false)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) builder.setMetered(false)
        configure?.let { builder.setConfigureIntent(it) }
        // A package uninstalled since it was picked throws, and would otherwise block every connect.
        val kept = s.excludedApps.filterTo(LinkedHashSet<String>()) {
            runCatching { builder.addDisallowedApplication(it) }.isSuccess
        }
        val tun = builder.establish() ?: return null
        return tun to kept
    }

    // The settings page's own rules decide what is carved out, so a range it refuses never becomes a route.
    private fun excludedRoutes(ranges: List<String>): List<Route> =
        ranges.mapNotNull { raw -> SplitTunnelRanges.route(raw.trim())?.let { (address, prefix) -> Route(address, prefix) } }

    // Android has no local resolver to reach here, and the WireGuard data plane carries IPv4 only.
    private fun usableResolver(raw: String): String? {
        val octets = raw.trim().split('.')
        if (octets.size != 4) return null
        val values = octets.map { octet ->
            if (octet.isEmpty() || octet.length > 3 || !octet.all { it in '0'..'9' }) return null
            octet.toInt().takeIf { it <= 255 } ?: return null
        }
        val first = values[0]
        return if (first == 0 || first == 127 || first >= 224) null else values.joinToString(".")
    }

    // Address and routes go in together or not at all: routes alone send IPv6 around the tunnel,
    // and a device with IPv6 off rejects the address.
    private fun VpnService.Builder.blackholeIpv6(allowLan: Boolean) {
        if (runCatching { addAddress(BLACKHOLE_V6, BLACKHOLE_PREFIX_V6) }.isFailure) return
        tunnelRoutesV6(allowLan).forEach { addRoute(it.address, it.prefixLength) }
    }
}
