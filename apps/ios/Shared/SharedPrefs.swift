import Foundation

// Preferences kept outside the Go core, in the app group so the extension reads the same values.
enum SharedPrefs {
    enum Key {
        static let killSwitch = "killSwitch"
        static let darkTheme = "darkTheme"
        static let autoConnect = "autoConnect"
        static let lockdown = "lockdown"
        static let connectionAlerts = "connectionAlerts"
        static let panicWipe = "panicWipe"
        static let accent = "accent"
        static let developerMode = "developerMode"
        static let splitTunnelEnabled = "splitTunnelEnabled"
        static let splitTunnelRanges = "splitTunnelRanges"
        static let recentRegions = "recentRegions"
        static let rotationVisited = "rotationVisited"
        static let pinnedServerId = "pinnedServerId"
        static let appInForeground = "appInForeground"
        static let multihopEnabled = "multihopEnabled"
        static let multihopEntryId = "multihopEntryId"
    }

    static let maxRecentRegions = 8

    static func bool(_ key: String, default fallback: Bool, in defaults: UserDefaults) -> Bool {
        defaults.object(forKey: key) as? Bool ?? fallback
    }

    static func strings(_ key: String, in defaults: UserDefaults) -> [String] {
        defaults.stringArray(forKey: key) ?? []
    }

    static func connectionAlerts(in defaults: UserDefaults) -> Bool {
        bool(Key.connectionAlerts, default: true, in: defaults)
    }

    // The ranges the tunnel should leave out, or none while split tunnelling is off.
    static func excludedRanges(in defaults: UserDefaults) -> [String] {
        guard bool(Key.splitTunnelEnabled, default: false, in: defaults) else { return [] }
        return strings(Key.splitTunnelRanges, in: defaults)
    }

    static func appInForeground(in defaults: UserDefaults) -> Bool {
        bool(Key.appInForeground, default: false, in: defaults)
    }

    // nil while multihop is off; "" means pick the entry automatically.
    static func multihopEntry(in defaults: UserDefaults) -> String? {
        guard bool(Key.multihopEnabled, default: false, in: defaults) else { return nil }
        return defaults.string(forKey: Key.multihopEntryId) ?? ""
    }
}
