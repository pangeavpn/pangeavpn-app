import Observation
import SwiftUI

// The five desktop accents; terra is the brand default.
enum Accent: String, CaseIterable, Sendable {
    case terra, purple, ocean, emerald, rose

    var hex: UInt32 {
        switch self {
        case .terra: return 0xC3562B
        case .purple: return 0x8451C9
        case .ocean: return 0x2B7BC3
        case .emerald: return 0x26804F
        case .rose: return 0xC32B5E
        }
    }

    var hoverHex: UInt32 {
        switch self {
        case .terra: return 0xA84A25
        case .purple: return 0x6F41B0
        case .ocean: return 0x226499
        case .emerald: return 0x1D6A40
        case .rose: return 0xA32450
        }
    }

    var color: Color { Color(hex: hex) }

    var hover: Color { Color(hex: hoverHex) }

    var title: String {
        switch self {
        case .terra: return String(localized: "accent_terra", defaultValue: "Terra")
        case .purple: return String(localized: "accent_purple", defaultValue: "Purple")
        case .ocean: return String(localized: "accent_ocean", defaultValue: "Ocean")
        case .emerald: return String(localized: "accent_emerald", defaultValue: "Emerald")
        case .rose: return String(localized: "accent_rose", defaultValue: "Rose")
        }
    }
}

// Read through Palette.accent, so every view that paints with it follows a change at once.
@Observable
final class AccentTheme: @unchecked Sendable {
    static let shared = AccentTheme()

    var accent = Accent.terra

    private init() {}
}
