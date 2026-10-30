import Foundation

enum TransportLabels {
    // Product names, so they read the same in every language, as on desktop.
    private static let names: [String: String] = [
        "cloak": "Cloak", "reality": "VLESS+REALITY", "shadowsocks": "Shadowsocks",
        "hysteria2": "Hysteria2", "anytls": "AnyTLS", "snowflake": "Snowflake",
        "naive": "NaiveProxy", "wireguard": "WireGuard",
    ]

    // Desktop's picker order, which is the order automatic mode walks.
    static let pickerOrder: [Transport] = [.auto, .reality, .cloak, .shadowsocks, .hysteria2, .anytls]

    static func status(_ activeTransport: String?) -> String {
        activeTransport.flatMap { names[$0] } ?? ""
    }

    static func choice(_ transport: Transport) -> String {
        switch transport {
        case .auto: return String(localized: "settings_transport_auto", defaultValue: "Automatic (recommended)")
        case .cloak: return String(localized: "settings_transport_cloak", defaultValue: "Cloak")
        case .reality: return String(localized: "settings_transport_reality", defaultValue: "VLESS+REALITY")
        case .shadowsocks: return String(localized: "settings_transport_shadowsocks", defaultValue: "Shadowsocks")
        case .hysteria2: return String(localized: "settings_transport_hysteria2", defaultValue: "Hysteria2")
        case .anytls: return String(localized: "settings_transport_anytls", defaultValue: "AnyTLS")
        }
    }

    static func description(_ transport: Transport) -> String {
        switch transport {
        case .auto:
            return String(localized: "bypass_auto_desc", defaultValue: "Tries each disguise in turn and remembers what worked on this network.")
        case .cloak:
            return String(localized: "bypass_cloak_desc", defaultValue: "TLS aimed at a decoy site, so the traffic looks like an ordinary visit there.")
        case .reality:
            return String(localized: "bypass_reality_desc", defaultValue: "Borrows a real site's TLS handshake, so probes see a genuine site. The strongest disguise.")
        case .shadowsocks:
            return String(localized: "bypass_shadowsocks_desc", defaultValue: "No TLS at all, so a block aimed at TLS misses it.")
        case .hysteria2:
            return String(localized: "bypass_hysteria2_desc", defaultValue: "UDP and QUIC instead of TCP. Worth a try when every TCP connection dies.")
        case .anytls:
            return String(localized: "bypass_anytls_desc", defaultValue: "A padded TLS session, smoothing the TLS-in-TLS shape other TLS transports leak.")
        }
    }

    // A stored choice iOS offers no row for (naive, wireguard) reads as automatic.
    static func selected(_ preferredTransport: String) -> Transport {
        Transport(rawValue: preferredTransport) ?? .auto
    }
}
