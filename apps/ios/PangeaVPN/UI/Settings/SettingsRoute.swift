import Foundation

// One entry per desktop settings rail item, plus the iOS-only kill switch and devices pages.
enum SettingsRoute: Hashable, CaseIterable, Sendable {
    case transport, provisioning, network, killSwitch, splitTunnel, startup
    case notifications, panic, language, appearance, support, update, developer, devices

    // The home list, in desktop's rail order; developer appears only once unlocked.
    static let home: [SettingsRoute] = [
        .transport, .provisioning, .network, .killSwitch, .splitTunnel, .startup,
        .notifications, .panic, .language, .appearance, .support, .update, .developer,
    ]

    var title: String {
        switch self {
        case .transport: return String(localized: "settings_transport_heading", defaultValue: "Censorship Bypass")
        case .provisioning: return String(localized: "settings_censorship_heading", defaultValue: "Provisioning")
        case .network: return String(localized: "settings_network_heading", defaultValue: "Network")
        case .killSwitch: return String(localized: "settings_section_kill_switch", defaultValue: "Kill Switch")
        case .splitTunnel: return String(localized: "settings_section_split_tunnel", defaultValue: "Split Tunnelling")
        case .startup: return String(localized: "settings_section_startup", defaultValue: "Startup")
        case .notifications: return String(localized: "settings_section_notifications", defaultValue: "Notifications")
        case .panic: return String(localized: "settings_section_panic", defaultValue: "Panic Wipe")
        case .language: return String(localized: "settings_section_language", defaultValue: "Language")
        case .appearance: return String(localized: "settings_section_appearance", defaultValue: "Appearance")
        case .support: return String(localized: "settings_section_support", defaultValue: "Support")
        case .update: return String(localized: "settings_section_update", defaultValue: "Software Update")
        case .developer: return String(localized: "settings_section_developer", defaultValue: "Developer Options")
        case .devices: return String(localized: "devices_title", defaultValue: "Devices")
        }
    }

    var systemImage: String {
        switch self {
        case .transport: return "shield"
        case .provisioning: return "server.rack"
        case .network: return "globe"
        case .killSwitch: return "lock.shield"
        case .splitTunnel: return "arrow.triangle.branch"
        case .startup: return "bolt"
        case .notifications: return "bell"
        case .panic: return "exclamationmark.triangle"
        case .language: return "character.bubble"
        case .appearance: return "paintpalette"
        case .support: return "lifepreserver"
        case .update: return "arrow.down.circle"
        case .developer: return "chevron.left.forwardslash.chevron.right"
        case .devices: return "laptopcomputer.and.iphone"
        }
    }
}
