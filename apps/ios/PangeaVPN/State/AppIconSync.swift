import Foundation
import os

extension Accent {
    // The Home Screen icon for this accent; terra is the app's primary icon.
    var iconName: String? {
        switch self {
        case .terra: return nil
        case .purple: return "AppIcon-Purple"
        case .ocean: return "AppIcon-Ocean"
        case .emerald: return "AppIcon-Emerald"
        case .rose: return "AppIcon-Rose"
        }
    }
}

@MainActor
protocol AppIconSwitching {
    var supportsAlternateIcons: Bool { get }
    var alternateIconName: String? { get }
    func setAlternateIconName(_ name: String?) async throws
}

@MainActor
enum AppIconSync {
    private static let log = Logger(subsystem: "org.pangeavpn", category: "app-icon")

    // iOS confirms every change with an alert, so the icon is only set when it differs.
    @discardableResult
    static func apply(_ accent: Accent, matching: Bool, on icons: some AppIconSwitching) async -> Bool {
        guard icons.supportsAlternateIcons else { return false }
        let wanted = matching ? accent.iconName : nil
        guard icons.alternateIconName != wanted else { return true }
        do {
            try await icons.setAlternateIconName(wanted)
            return true
        } catch {
            log.error("app icon change failed: \(error.localizedDescription, privacy: .public)")
            return false
        }
    }
}
