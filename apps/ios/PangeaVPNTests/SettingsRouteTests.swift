import XCTest
@testable import PangeaVPN

final class SettingsRouteTests: XCTestCase {
    func testWideSelectsTheFirstSectionWithoutADeepLink() {
        XCTAssertEqual(SettingsRoute.selected(in: []), .transport)
        XCTAssertEqual(SettingsRoute.pushed(in: []), [])
    }

    func testWideSelectsTheDeepLinkedPage() {
        XCTAssertEqual(SettingsRoute.selected(in: [.startup]), .startup)
        XCTAssertEqual(SettingsRoute.pushed(in: [.startup]), [])
    }

    func testWidePaneStacksOnlyWhatWasPushedPastTheSelection() {
        XCTAssertEqual(SettingsRoute.selected(in: [.support, .devices]), .support)
        XCTAssertEqual(SettingsRoute.pushed(in: [.support, .devices]), [.devices])
    }

    func testTurningDeveloperOffLeavesNoDeveloperPageInThePath() {
        XCTAssertEqual(SettingsRoute.withoutDeveloper([.developer]), [])
        XCTAssertEqual(SettingsRoute.withoutDeveloper([.network, .developer]), [.network])
        XCTAssertEqual(SettingsRoute.withoutDeveloper([.network]), [.network])
    }
}
