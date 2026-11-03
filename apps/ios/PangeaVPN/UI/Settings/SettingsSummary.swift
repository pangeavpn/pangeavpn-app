import Foundation

// The one-line value under each settings home row; each page file adds its own function.
@MainActor
enum SettingsSummary {
    static let separator = " \u{00B7} "

    static func off() -> String {
        String(localized: "settings_summary_off", defaultValue: "Off")
    }

    static func joined(_ parts: [String]) -> String {
        let shown = parts.filter { !$0.isEmpty }
        return shown.isEmpty ? off() : shown.joined(separator: separator)
    }
}
