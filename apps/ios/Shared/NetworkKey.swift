import Foundation

struct InterfaceAddress: Equatable, Sendable {
    let name: String
    let address: String
    let isIPv6: Bool
}

enum NetworkKey {
    private static let skippedPrefixes = ["utun", "lo", "ipsec", "awdl", "llw"]

    static func make(_ addrs: [InterfaceAddress]) -> String {
        addrs.compactMap(token).sorted().joined(separator: "|")
    }

    static func current() -> String {
        var head: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&head) == 0, let first = head else { return "" }
        defer { freeifaddrs(head) }

        var found: [InterfaceAddress] = []
        var cursor: UnsafeMutablePointer<ifaddrs>? = first
        while let entry = cursor {
            if let item = interfaceAddress(entry.pointee) { found.append(item) }
            cursor = entry.pointee.ifa_next
        }
        return make(found)
    }

    private static func interfaceAddress(_ entry: ifaddrs) -> InterfaceAddress? {
        let flags = Int32(entry.ifa_flags)
        guard flags & IFF_UP != 0, let sa = entry.ifa_addr else { return nil }
        let family = Int32(sa.pointee.sa_family)
        guard family == AF_INET || family == AF_INET6 else { return nil }

        var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
        let result = getnameinfo(
            sa, socklen_t(sa.pointee.sa_len), &host, socklen_t(host.count), nil, 0, NI_NUMERICHOST
        )
        guard result == 0 else { return nil }
        return InterfaceAddress(
            name: String(cString: entry.ifa_name),
            address: String(cString: host),
            isIPv6: family == AF_INET6
        )
    }

    private static func token(_ item: InterfaceAddress) -> String? {
        if skippedPrefixes.contains(where: { item.name.hasPrefix($0) }) { return nil }
        let literal = item.address.split(separator: "%", maxSplits: 1).first.map(String.init) ?? ""
        let body = item.isIPv6 ? ipv6Token(literal) : ipv4Token(literal)
        return body.map { "\(item.name):\($0)" }
    }

    private static func ipv4Token(_ literal: String) -> String? {
        var addr = in_addr()
        guard inet_pton(AF_INET, literal, &addr) == 1 else { return nil }
        let first = UInt8(truncatingIfNeeded: addr.s_addr)
        let second = UInt8(truncatingIfNeeded: addr.s_addr >> 8)
        if first == 0 || first == 127 || first >= 224 { return nil }
        if first == 169 && second == 254 { return nil }
        return literal
    }

    // Host bits rotate under privacy extensions, so only the /64 is stable.
    private static func ipv6Token(_ literal: String) -> String? {
        var addr = in6_addr()
        guard inet_pton(AF_INET6, literal, &addr) == 1 else { return nil }
        let bytes = withUnsafeBytes(of: &addr) { Array($0) }
        if bytes.allSatisfy({ $0 == 0 }) || bytes == [UInt8](repeating: 0, count: 15) + [1] { return nil }
        if bytes[0] == 0xff || (bytes[0] == 0xfe && bytes[1] & 0xc0 == 0x80) { return nil }
        let hextets = (0..<4).map { String(UInt16(bytes[$0 * 2]) << 8 | UInt16(bytes[$0 * 2 + 1]), radix: 16) }
        return hextets.joined(separator: ":") + ":0:0:0:0/64"
    }
}
