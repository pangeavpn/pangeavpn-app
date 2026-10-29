import XCTest
@testable import PangeaVPN

final class TokenInputTests: XCTestCase {
    func testKeepsFormattedAccountNumber() {
        let value = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"
        XCTAssertEqual(TokenInput.sanitize(value), value)
    }

    func testKeepsLegacyNumericToken() {
        let value = "0123456789012345"
        XCTAssertEqual(TokenInput.sanitize(value), value)
    }

    func testDropsWhitespaceAndSymbols() {
        XCTAssertEqual(TokenInput.sanitize(" ab cd\n!@#_\t12"), "abcd12")
    }

    func testDropsNonAsciiDigitsAndLetters() {
        XCTAssertEqual(TokenInput.sanitize("١٢٣٤éAB"), "AB")
    }

    func testCapsSymbolCountButNotDashes() {
        let long = String(repeating: "A", count: 5000)
        XCTAssertEqual(TokenInput.sanitize(long).count, TokenInput.maxSymbols)
        let dashed = String(repeating: "AAAA-", count: 16)
        XCTAssertEqual(TokenInput.sanitize(dashed), dashed)
    }

    func testEmpty() {
        XCTAssertEqual(TokenInput.sanitize(""), "")
    }
}
