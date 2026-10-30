import Foundation
import NetworkExtension
import os

// Everything that shapes an applied configuration, so an unchanged one is never applied twice.
struct TunnelSettingsFingerprint: Equatable {
    let address: String
    let prefixLength: Int
    let dns: [String]
    let mtu: Int
    let allowLan: Bool
    let excludedRanges: [String]
}

enum TunnelSettingsBuilder {
    static let remoteAddress = "127.0.0.1"
    // Documentation addresses reach nothing, so traffic fails closed until the real config lands.
    static let blockingConfig = TunnelConfig(
        address: "192.0.2.2", prefixLength: 32, dns: ["192.0.2.1"], mtu: 1280, serverId: "", serverName: "")
    static let minExcludedPrefix = 8

    // IPv6 is captured and dropped, so it can never leak around the tunnel.
    private static let blackholeV6 = "fd00::1"
    private static let blackholePrefixV6 = 128
    private static let hostMaskV4 = "255.255.255.255"
    private static let log = Logger(subsystem: "org.pangeavpn", category: "tunnel")

    private static let lanV4: [(address: String, prefix: Int)] = [
        ("10.0.0.0", 8), ("172.16.0.0", 12), ("192.168.0.0", 16), ("169.254.0.0", 16),
    ]
    private static let lanV6: [(address: String, prefix: Int)] = [("fc00::", 7), ("fe80::", 10)]

    static func build(
        _ c: TunnelConfig, allowLan: Bool, excludedRanges: [String] = []
    ) throws -> NEPacketTunnelNetworkSettings {
        guard !dnsServers(c).isEmpty else { throw TunnelStartError.invalidDNS }
        return make(c, allowLan: allowLan, excludedRanges: excludedRanges)
    }

    static func make(
        _ c: TunnelConfig, allowLan: Bool, excludedRanges: [String] = []
    ) -> NEPacketTunnelNetworkSettings {
        let resolvers = dnsServers(c)
        let excluded = excludedRoutes(excludedRanges)
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: remoteAddress)
        settings.ipv4Settings = ipv4(c, resolvers: resolvers, allowLan: allowLan, excluded: excluded)
        settings.ipv6Settings = ipv6(allowLan: allowLan)
        settings.dnsSettings = dns(resolvers)
        settings.mtu = NSNumber(value: c.mtu)
        return settings
    }

    static func blocking(allowLan: Bool, excludedRanges: [String] = []) -> NEPacketTunnelNetworkSettings {
        make(blockingConfig, allowLan: allowLan, excludedRanges: excludedRanges)
    }

    static func fingerprint(
        _ c: TunnelConfig, allowLan: Bool, excludedRanges: [String]
    ) -> TunnelSettingsFingerprint {
        TunnelSettingsFingerprint(
            address: c.address, prefixLength: c.prefixLength, dns: dnsServers(c), mtu: c.mtu,
            allowLan: allowLan, excludedRanges: excludedRanges)
    }

    // iOS has no local resolver, and the WireGuard data plane carries IPv4 only.
    static func dnsServers(_ c: TunnelConfig) -> [String] {
        c.dns.compactMap(usableResolver)
    }

    static func mask(prefixLength: Int) -> String {
        dotted(prefixBits(min(max(prefixLength, 0), 32)))
    }

    // An IPv4 range as "a.b.c.d/n" or a bare address, with the host bits cleared.
    static func parseRange(_ raw: String) -> (address: String, prefix: Int)? {
        let parts = raw.trimmingCharacters(in: .whitespaces).split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count == 1 || parts.count == 2 else { return nil }
        var addr = in_addr()
        guard inet_pton(AF_INET, String(parts[0]), &addr) == 1 else { return nil }
        let parsedPrefix: Int? = parts.count == 2 ? Int(parts[1]) : 32
        guard let prefix = parsedPrefix, (minExcludedPrefix...32).contains(prefix) else { return nil }
        let network = UInt32(bigEndian: addr.s_addr) & prefixBits(prefix)
        return (dotted(network), prefix)
    }

    static func excludedRoutes(_ ranges: [String]) -> [NEIPv4Route] {
        ranges.compactMap { raw -> NEIPv4Route? in
            guard let range = parseRange(raw) else {
                log.error("skipping an unusable excluded range: \(raw, privacy: .private)")
                return nil
            }
            return route4(range)
        }
    }

    private static func prefixBits(_ prefix: Int) -> UInt32 {
        prefix <= 0 ? 0 : UInt32.max << (32 - min(prefix, 32))
    }

    private static func dotted(_ bits: UInt32) -> String {
        [24, 16, 8, 0].map { String((bits >> $0) & 0xFF) }.joined(separator: ".")
    }

    private static func usableResolver(_ raw: String) -> String? {
        let literal = raw.trimmingCharacters(in: .whitespaces)
        var addr = in_addr()
        guard inet_pton(AF_INET, literal, &addr) == 1 else { return nil }
        let first = withUnsafeBytes(of: &addr) { $0[0] }
        return first == 0 || first == 127 || first >= 224 ? nil : literal
    }

    private static func ipv4(
        _ c: TunnelConfig, resolvers: [String], allowLan: Bool, excluded: [NEIPv4Route]
    ) -> NEIPv4Settings {
        let v4 = NEIPv4Settings(addresses: [c.address], subnetMasks: [mask(prefixLength: c.prefixLength)])
        let carved = (allowLan ? lanV4.map(route4) : []) + excluded
        // A carved-out range could otherwise send a resolver around the tunnel.
        let pinned = carved.isEmpty ? [] : resolvers.map { NEIPv4Route(destinationAddress: $0, subnetMask: hostMaskV4) }
        v4.includedRoutes = [NEIPv4Route.default()] + pinned
        v4.excludedRoutes = carved
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
