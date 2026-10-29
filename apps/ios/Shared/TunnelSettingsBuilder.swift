import Foundation
import NetworkExtension

enum TunnelSettingsBuilder {
    static let remoteAddress = "127.0.0.1"

    // IPv6 is captured and dropped, so it can never leak around the tunnel.
    private static let blackholeV6 = "fd00::1"
    private static let blackholePrefixV6 = 128
    private static let hostMaskV4 = "255.255.255.255"

    private static let lanV4: [(address: String, prefix: Int)] = [
        ("10.0.0.0", 8), ("172.16.0.0", 12), ("192.168.0.0", 16), ("169.254.0.0", 16),
    ]
    private static let lanV6: [(address: String, prefix: Int)] = [("fc00::", 7), ("fe80::", 10)]

    static func build(_ c: TunnelConfig, allowLan: Bool) throws -> NEPacketTunnelNetworkSettings {
        guard !dnsServers(c).isEmpty else { throw TunnelStartError.invalidDNS }
        return make(c, allowLan: allowLan)
    }

    static func make(_ c: TunnelConfig, allowLan: Bool) -> NEPacketTunnelNetworkSettings {
        let resolvers = dnsServers(c)
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: remoteAddress)
        settings.ipv4Settings = ipv4(c, resolvers: resolvers, allowLan: allowLan)
        settings.ipv6Settings = ipv6(allowLan: allowLan)
        settings.dnsSettings = dns(resolvers)
        settings.mtu = NSNumber(value: c.mtu)
        return settings
    }

    // iOS has no local resolver, and the WireGuard data plane carries IPv4 only.
    static func dnsServers(_ c: TunnelConfig) -> [String] {
        c.dns.compactMap(usableResolver)
    }

    static func mask(prefixLength: Int) -> String {
        let prefix = min(max(prefixLength, 0), 32)
        let bits: UInt32 = prefix == 0 ? 0 : UInt32.max << (32 - prefix)
        return [24, 16, 8, 0].map { String((bits >> $0) & 0xFF) }.joined(separator: ".")
    }

    private static func usableResolver(_ raw: String) -> String? {
        let literal = raw.trimmingCharacters(in: .whitespaces)
        var addr = in_addr()
        guard inet_pton(AF_INET, literal, &addr) == 1 else { return nil }
        let first = withUnsafeBytes(of: &addr) { $0[0] }
        return first == 0 || first == 127 || first >= 224 ? nil : literal
    }

    private static func ipv4(_ c: TunnelConfig, resolvers: [String], allowLan: Bool) -> NEIPv4Settings {
        let v4 = NEIPv4Settings(addresses: [c.address], subnetMasks: [mask(prefixLength: c.prefixLength)])
        // Excluding the LAN would otherwise send a private resolver around the tunnel.
        let pinned = allowLan ? resolvers.map { NEIPv4Route(destinationAddress: $0, subnetMask: hostMaskV4) } : []
        v4.includedRoutes = [NEIPv4Route.default()] + pinned
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

    private static func dns(_ resolvers: [String]) -> NEDNSSettings {
        let dns = NEDNSSettings(servers: resolvers)
        dns.matchDomains = [""]
        return dns
    }
}

// The system may never call back from a settings apply, so a watchdog fails the step instead.
enum SettingsApply {
    static func run(
        timeout: TimeInterval,
        scheduler: LifecycleScheduler,
        apply: (@escaping (Error?) -> Void) -> Void,
        completion: @escaping (Error?) -> Void
    ) {
        let gate = OnceCompletion(completion)
        let watchdog = scheduler.after(timeout) { gate.fire(TunnelStartError.settingsTimeout) }
        apply { error in
            watchdog.cancel()
            gate.fire(error)
        }
    }
}

final class OnceCompletion: @unchecked Sendable {
    private let lock = NSLock()
    private var handler: ((Error?) -> Void)?

    init(_ handler: @escaping (Error?) -> Void) {
        self.handler = handler
    }

    func fire(_ error: Error?) {
        let pending: ((Error?) -> Void)? = lock.withLock {
            let current = handler
            handler = nil
            return current
        }
        pending?(error)
    }
}
