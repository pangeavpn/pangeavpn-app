import XCTest
@testable import PangeaVPN

final class LoadToneTests: XCTestCase {
    func testThresholds() {
        XCTAssertEqual(LoadTone.color(forPercent: 0), Palette.connected)
        XCTAssertEqual(LoadTone.color(forPercent: 39), Palette.connected)
        XCTAssertEqual(LoadTone.color(forPercent: 40), Palette.connecting)
        XCTAssertEqual(LoadTone.color(forPercent: 74), Palette.connecting)
        XCTAssertEqual(LoadTone.color(forPercent: 75), Palette.error)
        XCTAssertEqual(LoadTone.color(forPercent: 500), Palette.error)
        XCTAssertEqual(LoadTone.color(forPercent: -5), Palette.connected)
    }
}
