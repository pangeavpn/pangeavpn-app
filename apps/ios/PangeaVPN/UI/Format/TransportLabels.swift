import Foundation

enum TransportLabels {
    // Product names, so they read the same in every language, as on desktop.
    private static let names: [String: String] = [
        "cloak": "Cloak", "reality": "VLESS+REALITY", "shadowsocks": "Shadowsocks",
        "hysteria2": "Hysteria2", "anytls": "AnyTLS", "snowflake": "Snowflake",
        "naive": "NaiveProxy", "wireguard": "WireGuard",
    ]

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
}
