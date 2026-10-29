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

    func testBackgroundFollowsAppearance() {
        XCTAssertEqual(rgb(Palette.bg, .dark), [0x14, 0x13, 0x12])
        XCTAssertEqual(rgb(Palette.bg, .light), [0xFB, 0xF9, 0xF2])
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
