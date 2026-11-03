import XCTest
@testable import PangeaVPN

final class SettingsInputTests: XCTestCase {
    func testKeepsAsciiDigits() {
        XCTAssertEqual(SettingsInput.asciiDigits("1380"), "1380")
    }

    func testDropsNonDigits() {
        XCTAssertEqual(SettingsInput.asciiDigits("13a8 0-"), "1380")
    }

    func testMapsPersianDigits() {
        XCTAssertEqual(SettingsInput.asciiDigits("۱۳۸۰"), "1380")
    }

    func testMapsArabicIndicDigits() {
        XCTAssertEqual(SettingsInput.asciiDigits("١٢٨٠"), "1280")
    }

    func testEmptyStaysEmpty() {
        XCTAssertEqual(SettingsInput.asciiDigits(""), "")
    }

    func testMtuRangeMatchesTheCore() {
        XCTAssertEqual(TunnelMtu.range, 1280...1420)
    }

    func testMtuAcceptsTheRangeEndsAndTheDefault() {
        XCTAssertEqual(TunnelMtu.parse("1280"), 1280)
        XCTAssertEqual(TunnelMtu.parse("1420"), 1420)
        XCTAssertEqual(TunnelMtu.parse("1380"), 1380)
        XCTAssertEqual(TunnelMtu.parse(" 1300 "), 1300)
        XCTAssertEqual(TunnelMtu.parse("01380"), 1380)
    }

    func testMtuRejectsValuesOutsideTheRange() {
        for input in ["1279", "1421", "0", "9999", "99999999999999999999999"] {
            XCTAssertNil(TunnelMtu.parse(input), input)
        }
    }

    func testMtuRejectsAnythingButAWholeNumber() {
        for input in ["", " ", "13a0", "1380.5", "-1300", "+1300", "۱۳۸۰"] {
            XCTAssertNil(TunnelMtu.parse(input), input)
        }
    }

    func testTypedMtuDigitsParseOnceMapped() {
        XCTAssertEqual(TunnelMtu.parse(SettingsInput.asciiDigits("۱۳۸۰")), 1380)
    }
}
