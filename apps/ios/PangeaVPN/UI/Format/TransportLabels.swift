import Foundation

enum TransportLabels {
    static func status(_ activeTransport: String?) -> String {
        switch activeTransport {
        case "cloak": return String(localized: "transport_status_cloak", defaultValue: "Obfuscation: Cloak")
        case "reality": return String(localized: "transport_status_reality", defaultValue: "Obfuscation: VLESS+REALITY")
        case "shadowsocks": return String(localized: "transport_status_shadowsocks", defaultValue: "Obfuscation: Shadowsocks")
        case "hysteria2": return String(localized: "transport_status_hysteria2", defaultValue: "Obfuscation: Hysteria2")
        case "anytls": return String(localized: "transport_status_anytls", defaultValue: "Obfuscation: AnyTLS")
        case "snowflake": return String(localized: "transport_status_snowflake", defaultValue: "Obfuscation: Snowflake")
        case "naive": return String(localized: "transport_status_naive", defaultValue: "Obfuscation: NaiveProxy")
        default: return String(localized: "transport_status_none", defaultValue: "")
        }
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
