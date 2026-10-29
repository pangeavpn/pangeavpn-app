import SwiftUI
import UIKit

enum Palette {
    static var accent: Color { AccentTheme.shared.accent.color }
    static var accentHover: Color { AccentTheme.shared.accent.hover }
    static let onAccent = Color(hex: 0xFBF9F2)

    static let bg = dynamic(dark: 0x141312, light: 0xFBF9F2)
    static let surface = dynamic(dark: 0x1E1C1C, light: 0xFFFFFF)
    static let elevated = dynamic(dark: 0x272424, light: 0xFFFFFF)
    static let text = dynamic(dark: 0xFBF9F2, light: 0x1E1C1C)
    static let textSecondary = dynamic(dark: 0xAEA299, light: 0x5F5B53)
    static let textMuted = dynamic(dark: 0x84847C, light: 0x6D685F)
    static let border = dynamic(dark: 0x332F2F, light: 0xDDD8CF)
    static let borderSubtle = dynamic(dark: 0x272424, light: 0xEEE9E0)

    static let connected = Color(hex: 0x10B981)
    static let connecting = Color(hex: 0xF59E0B)
    static let error = Color(hex: 0xF43F5E)
    static let off = Color(hex: 0x5A5654)

    static func state(_ s: TunnelState) -> Color {
        switch s {
        case .connected: return connected
        case .connecting, .disconnecting: return connecting
        case .error: return error
        case .disconnected: return off
        }
    }

    private static func dynamic(dark: UInt32, light: UInt32) -> Color {
        Color(UIColor { traits in
            UIColor(hex: traits.userInterfaceStyle == .dark ? dark : light)
        })
    }
}

extension UIColor {
    convenience init(hex: UInt32) {
        self.init(
            red: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: 1
        )
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(UIColor(hex: hex))
    }
}
