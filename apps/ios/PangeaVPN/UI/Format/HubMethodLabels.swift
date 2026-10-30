import Foundation

// How a probe result reads: it worked, it had nothing to try, or the network failed it.
enum HubTestTone: Equatable, Sendable {
    case ok, unavailable, failed
}

enum HubMethodLabels {
    // Desktop's order, which is the order the core tries them in.
    static let order = ["directIp", "reality", "shadowsocks", "fronted", "normal"]

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

    static func isEnabled(_ method: String, in methods: HubMethods) -> Bool {
        switch method {
        case "directIp": return methods.directIp
        case "reality": return methods.reality
        case "shadowsocks": return methods.shadowsocks
        case "fronted": return methods.fronted
        case "normal": return methods.normal
        default: return false
        }
    }

    static func enabled(in methods: HubMethods) -> [String] {
        order.filter { isEnabled($0, in: methods) }
    }

    static func activeText(_ status: HubStatus) -> String {
        guard let active = status.active, !active.isEmpty else {
            return String(localized: "provisioning_active_none", defaultValue: "No method in use yet")
        }
        if let detail = status.detail, !detail.isEmpty {
            return String(format: String(localized: "provisioning_active_detail", defaultValue: "In use: %@ — %@"), title(active), detail)
        }
        return String(format: String(localized: "provisioning_active_using", defaultValue: "In use: %@"), title(active))
    }

    static func testText(_ result: HubTestResult) -> String {
        if let reason = unavailableText(result) { return reason }
        guard result.ok else {
            return String(localized: "provisioning_result_fail", defaultValue: "Could not reach our servers")
        }
        if !result.detail.isEmpty {
            return String(format: String(localized: "provisioning_result_ok_detail", defaultValue: "Works via %@ (%lld ms)"), result.detail, result.ms)
        }
        return String(format: String(localized: "provisioning_result_ok", defaultValue: "Works (%lld ms)"), result.ms)
    }

    static func tone(_ result: HubTestResult) -> HubTestTone {
        if unavailableText(result) != nil { return .unavailable }
        return result.ok ? .ok : .failed
    }

    // A method with nothing to try says why, rather than reading as a network failure.
    private static func unavailableText(_ result: HubTestResult) -> String? {
        switch result.unavailable {
        case "noAddress":
            return String(localized: "provisioning_result_no_address", defaultValue: "No address stored yet, and encrypted DNS is off")
        case "noCredentials" where result.method == "reality":
            return String(localized: "provisioning_result_no_reality_credentials", defaultValue: "No REALITY credentials stored yet")
        case "noCredentials":
            return String(localized: "provisioning_result_no_credentials", defaultValue: "No Shadowsocks credentials stored yet")
        case "noRelay":
            return String(localized: "provisioning_result_no_relay", defaultValue: "No relay configured yet")
        case "busy":
            return String(localized: "provisioning_result_busy", defaultValue: "Another test is still running")
        default:
            return nil
        }
    }
}
