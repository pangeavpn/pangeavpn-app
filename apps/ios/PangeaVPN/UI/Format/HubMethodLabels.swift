import Foundation

enum HubMethodLabels {
    static func title(_ method: String) -> String {
        switch method {
        case "directIp": return String(localized: "settings_censorship_directip_title", defaultValue: "Direct IP")
        case "reality": return String(localized: "settings_censorship_reality_title", defaultValue: "REALITY")
        case "shadowsocks": return String(localized: "settings_censorship_shadowsocks_title", defaultValue: "Shadowsocks")
        case "fronted": return String(localized: "settings_censorship_fronted_title", defaultValue: "Relay")
        case "normal": return String(localized: "settings_censorship_normal_title", defaultValue: "Normal connection")
        default: return method
        }
    }

    static func hint(_ method: String) -> String {
        switch method {
        case "directIp":
            return String(localized: "settings_censorship_directip_hint", defaultValue: "Connects by IP, no DNS lookup.")
        case "reality":
            return String(localized: "settings_censorship_reality_hint", defaultValue: "Tunnels account traffic through REALITY, which looks like a visit to an ordinary website.")
        case "shadowsocks":
            return String(localized: "settings_censorship_shadowsocks_hint", defaultValue: "Tunnels account traffic through Shadowsocks, so nobody sees which server you reach.")
        case "fronted":
            return String(localized: "settings_censorship_fronted_hint", defaultValue: "Goes through a relay on a large content network, at an address too widely shared to block cheaply. The relay can't read what it passes on.")
        case "normal":
            return String(localized: "settings_censorship_normal_hint", defaultValue: "Plain HTTPS and a normal DNS lookup. Tried last: it's the only one that exposes our domain.")
        default: return ""
        }
    }
}
