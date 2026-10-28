import NetworkExtension

enum TunnelSettingsBuilder {
    static let remoteAddress = "127.0.0.1"

    // IPv6 is captured and dropped, so it can never leak around the tunnel.
    private static let blackholeV6 = "fd00::1"
    private static let blackholePrefixV6 = 128

    private static let lanV4: [(address: String, prefix: Int)] = [
        ("10.0.0.0", 8), ("172.16.0.0", 12), ("192.168.0.0", 16), ("169.254.0.0", 16),
    ]
    private static let lanV6: [(address: String, prefix: Int)] = [("fc00::", 7), ("fe80::", 10)]

    static func make(_ c: TunnelConfig, allowLan: Bool) -> NEPacketTunnelNetworkSettings {
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: remoteAddress)
        settings.ipv4Settings = ipv4(c, allowLan: allowLan)
        settings.ipv6Settings = ipv6(allowLan: allowLan)
        settings.dnsSettings = dns(c)
        settings.mtu = NSNumber(value: c.mtu)
        return settings
    }

    static func mask(prefixLength: Int) -> String {
        let prefix = min(max(prefixLength, 0), 32)
        let bits: UInt32 = prefix == 0 ? 0 : UInt32.max << (32 - prefix)
        return [24, 16, 8, 0].map { String((bits >> $0) & 0xFF) }.joined(separator: ".")
    }

    private static func ipv4(_ c: TunnelConfig, allowLan: Bool) -> NEIPv4Settings {
        let v4 = NEIPv4Settings(addresses: [c.address], subnetMasks: [mask(prefixLength: c.prefixLength)])
        v4.includedRoutes = [NEIPv4Route.default()]
        v4.excludedRoutes = allowLan ? lanV4.map(route4) : []
        return v4
    }

    private static func ipv6(allowLan: Bool) -> NEIPv6Settings {
        let v6 = NEIPv6Settings(
            addresses: [blackholeV6], networkPrefixLengths: [NSNumber(value: blackholePrefixV6)])
        v6.includedRoutes = [NEIPv6Route.default()]
        v6.excludedRoutes = allowLan ? lanV6.map(route6) : []
        return v6
    }

    private static func route4(_ r: (address: String, prefix: Int)) -> NEIPv4Route {
        NEIPv4Route(destinationAddress: r.address, subnetMask: mask(prefixLength: r.prefix))
    }

    private static func route6(_ r: (address: String, prefix: Int)) -> NEIPv6Route {
        NEIPv6Route(destinationAddress: r.address, networkPrefixLength: NSNumber(value: r.prefix))
    }

    private static func dns(_ c: TunnelConfig) -> NEDNSSettings {
        let dns = NEDNSSettings(servers: c.dns)
        dns.matchDomains = [""]
        return dns
    }
}
