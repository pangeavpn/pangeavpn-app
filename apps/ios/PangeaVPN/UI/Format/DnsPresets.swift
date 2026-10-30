import Foundation

// Desktop's DNS provider choices; automatic means no override, custom means a typed list.
enum DnsChoice: String, CaseIterable, Sendable {
    case automatic, adguard, cloudflare, quad9, cloudflareFamily, google, custom
}

enum DnsPresets {
    static func servers(for choice: DnsChoice) -> [String]? {
        switch choice {
        case .automatic: return []
        case .adguard: return ["94.140.14.14", "94.140.15.15"]
        case .cloudflare: return ["1.1.1.1", "1.0.0.1"]
        case .quad9: return ["9.9.9.9", "149.112.112.112"]
        case .cloudflareFamily: return ["1.1.1.3", "1.0.0.3"]
        case .google: return ["8.8.8.8", "8.8.4.4"]
        case .custom: return nil
        }
    }

    // Exact match, order included, so a reordered preset reads as custom just as on desktop.
    static func choice(for stored: [String]) -> DnsChoice {
        if stored.isEmpty { return .automatic }
        return DnsChoice.allCases.first { $0 != .automatic && servers(for: $0) == stored } ?? .custom
    }

    static func title(_ choice: DnsChoice) -> String {
        switch choice {
        case .automatic: return String(localized: "network_dns_automatic", defaultValue: "Automatic (VPN default)")
        case .adguard: return String(localized: "network_dns_adguard", defaultValue: "AdGuard DNS - Ads & trackers")
        case .cloudflare: return String(localized: "network_dns_cloudflare", defaultValue: "Cloudflare")
        case .quad9: return String(localized: "network_dns_quad9", defaultValue: "Quad9 - Malware blocking")
        case .cloudflareFamily: return String(localized: "network_dns_cloudflare_family", defaultValue: "Cloudflare Family - Malware & adult content")
        case .google: return String(localized: "network_dns_google", defaultValue: "Google Public DNS")
        case .custom: return String(localized: "network_dns_custom", defaultValue: "Custom")
        }
    }

    // The core silently drops bad entries, so the page must refuse them first. nil means invalid.
    static func normalize(_ text: String) -> [String]? {
        let parts = text.split(whereSeparator: { $0.isWhitespace || $0 == "," })
        var servers: [String] = []
        for part in parts {
            guard let address = ipv4(String(part)), !unroutable(address) else { return nil }
            let literal = dotted(address)
            if !servers.contains(literal) { servers.append(literal) }
        }
        return servers
    }

    // Keyboards in Arabic and Persian type their own digits; the core reads only ASCII ones.
    static func typed(_ text: String) -> String {
        String(text.map { character -> Character in
            guard !character.isASCII, let digit = character.wholeNumberValue, (0...9).contains(digit) else {
                return character
            }
            return Character(String(digit))
        })
    }

    // Unspecified, LAN, CGNAT, link-local, multicast and broadcast, plus loopback: iOS runs no local resolver.
    private static let blocked: [(base: UInt32, bits: Int)] = [
        (0x0000_0000, 8), (0x0A00_0000, 8), (0x6440_0000, 10), (0xA9FE_0000, 16),
        (0xAC10_0000, 12), (0xC0A8_0000, 16), (0xE000_0000, 3), (0x7F00_0000, 8),
    ]

    private static func ipv4(_ text: String) -> UInt32? {
        let octets = text.split(separator: ".", omittingEmptySubsequences: false)
        guard octets.count == 4 else { return nil }
        var value: UInt32 = 0
        for octet in octets {
            guard (1...3).contains(octet.count),
                  octet.unicodeScalars.allSatisfy({ ("0"..."9").contains($0) }),
                  let number = UInt8(octet) else { return nil }
            value = value << 8 | UInt32(number)
        }
        return value
    }

    private static func unroutable(_ address: UInt32) -> Bool {
        blocked.contains { range in
            let mask = UInt32.max << UInt32(32 - range.bits)
            return address & mask == range.base
        }
    }

    private static func dotted(_ address: UInt32) -> String {
        [24, 16, 8, 0].map { String((address >> UInt32($0)) & 0xFF) }.joined(separator: ".")
    }
}
