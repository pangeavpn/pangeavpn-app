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
}
