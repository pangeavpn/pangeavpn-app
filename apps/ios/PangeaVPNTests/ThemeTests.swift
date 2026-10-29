import SwiftUI
import UIKit
import XCTest
@testable import PangeaVPN

final class ThemeTests: XCTestCase {
    private func rgb(_ color: Color, _ style: UIUserInterfaceStyle) -> [CGFloat] {
        let resolved = UIColor(color).resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        resolved.getRed(&r, green: &g, blue: &b, alpha: &a)
        return [r, g, b].map { ($0 * 255).rounded() }
    }

    private func hex(_ v: UInt32) -> [CGFloat] {
        [CGFloat((v >> 16) & 0xFF), CGFloat((v >> 8) & 0xFF), CGFloat(v & 0xFF)]
    }

    func testPaletteMatchesDesignValuesInBothAppearances() {
        let pairs: [(String, Color, UInt32, UInt32)] = [
            ("bg", Palette.bg, 0x141312, 0xFBF9F2),
            ("surface", Palette.surface, 0x1E1C1C, 0xFFFFFF),
            ("elevated", Palette.elevated, 0x272424, 0xFFFFFF),
            ("text", Palette.text, 0xFBF9F2, 0x1E1C1C),
            ("textSecondary", Palette.textSecondary, 0xAEA299, 0x5F5B53),
            ("textMuted", Palette.textMuted, 0x84847C, 0x6D685F),
            ("border", Palette.border, 0x332F2F, 0xDDD8CF),
            ("borderSubtle", Palette.borderSubtle, 0x272424, 0xEEE9E0),
            ("accent", Palette.accent, 0xC3562B, 0xC3562B),
            ("accentHover", Palette.accentHover, 0xA84A25, 0xA84A25),
            ("onAccent", Palette.onAccent, 0xFBF9F2, 0xFBF9F2),
            ("connected", Palette.connected, 0x10B981, 0x10B981),
            ("connecting", Palette.connecting, 0xF59E0B, 0xF59E0B),
            ("error", Palette.error, 0xF43F5E, 0xF43F5E),
            ("off", Palette.off, 0x5A5654, 0x5A5654),
        ]
        for (name, color, dark, light) in pairs {
            XCTAssertEqual(rgb(color, .dark), hex(dark), "\(name) dark")
            XCTAssertEqual(rgb(color, .light), hex(light), "\(name) light")
        }
    }

    func testEaseMatchesFastOutSlowIn() {
        XCTAssertEqual(DriftTween.ease(0), 0, accuracy: 1e-9)
        XCTAssertEqual(DriftTween.ease(1), 1, accuracy: 1e-9)
        XCTAssertEqual(DriftTween.ease(0.5), 0.77556, accuracy: 0.005)
        XCTAssertEqual(DriftTween.ease(0.25), 0.23659, accuracy: 0.005)
        XCTAssertEqual(DriftTween.ease(-1), 0, accuracy: 1e-9)
        XCTAssertEqual(DriftTween.ease(2), 1, accuracy: 1e-9)
    }

    func testStateMapping() {
        XCTAssertEqual(rgb(Palette.state(.connected), .dark), rgb(Palette.connected, .dark))
        XCTAssertEqual(rgb(Palette.state(.connecting), .dark), rgb(Palette.connecting, .dark))
        XCTAssertEqual(rgb(Palette.state(.disconnecting), .dark), rgb(Palette.connecting, .dark))
        XCTAssertEqual(rgb(Palette.state(.error), .dark), rgb(Palette.error, .dark))
        XCTAssertEqual(rgb(Palette.state(.disconnected), .dark), rgb(Palette.off, .dark))
    }

    func testDriftTargets() {
        XCTAssertEqual(DriftLook.target(for: .connected).spread, 0)
        XCTAssertEqual(DriftLook.target(for: .connecting).spread, 0.12)
        XCTAssertEqual(DriftLook.target(for: .disconnecting), DriftLook.target(for: .disconnected))
        XCTAssertEqual(DriftLook.target(for: .error).alpha, 0.55)
    }

    func testTweenEndsAtTarget() {
        let start = Date()
        let tween = DriftTween(
            from: DriftLook.target(for: .disconnected),
            to: DriftLook.target(for: .connected),
            start: start
        )
        XCTAssertEqual(tween.look(at: start, animated: true), DriftLook.target(for: .disconnected))
        XCTAssertEqual(tween.look(at: start.addingTimeInterval(2), animated: true), DriftLook.target(for: .connected))
        XCTAssertEqual(tween.look(at: start, animated: false), DriftLook.target(for: .connected))
    }
}
