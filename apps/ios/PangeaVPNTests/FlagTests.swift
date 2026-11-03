import XCTest
@testable import PangeaVPN

final class FlagTests: XCTestCase {
    private let globe = "\u{1F310}"

    func testUppercaseCode() {
        XCTAssertEqual(Flag.emoji("GB"), "\u{1F1EC}\u{1F1E7}")
        XCTAssertEqual(Flag.emoji("US"), "\u{1F1FA}\u{1F1F8}")
    }

    func testLowercaseAndMixedCase() {
        XCTAssertEqual(Flag.emoji("de"), "\u{1F1E9}\u{1F1EA}")
        XCTAssertEqual(Flag.emoji("Fr"), "\u{1F1EB}\u{1F1F7}")
    }

    func testSurroundingWhitespaceIsTrimmed() {
        XCTAssertEqual(Flag.emoji(" jp "), "\u{1F1EF}\u{1F1F5}")
    }

    func testUKIsNotRemapped() {
        XCTAssertEqual(Flag.emoji("UK"), "\u{1F1FA}\u{1F1F0}")
        XCTAssertNotEqual(Flag.emoji("UK"), Flag.emoji("GB"))
    }

    func testEmptyGivesGlobe() {
        XCTAssertEqual(Flag.emoji(""), globe)
        XCTAssertEqual(Flag.emoji("   "), globe)
    }

    func testWrongLengthGivesGlobe() {
        XCTAssertEqual(Flag.emoji("G"), globe)
        XCTAssertEqual(Flag.emoji("GBR"), globe)
    }

    func testNonLettersGiveGlobe() {
        XCTAssertEqual(Flag.emoji("12"), globe)
        XCTAssertEqual(Flag.emoji("G1"), globe)
        XCTAssertEqual(Flag.emoji("--"), globe)
        XCTAssertEqual(Flag.emoji("\u{00E9}\u{00E9}"), globe)
    }
}
