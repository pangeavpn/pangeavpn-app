import XCTest
@testable import PangeaVPN

final class AccountNumberTests: XCTestCase {
    func testLegacyDigitTokensPassThroughUntouched() {
        XCTAssertEqual(AccountNumber.normalize("0123456789012345"), "0123456789012345")
        XCTAssertEqual(AccountNumber.format("0123456789012345"), "0123456789012345")
        XCTAssertEqual(AccountNumber.format("0123"), "0123")
    }

    func testNormalizeUppercasesStripsSeparatorsAndFoldsOIL() {
        XCTAssertEqual(
            AccountNumber.normalize("abcd-efgh jkmn-pqrs-tvwx-yz01"),
            "ABCDEFGHJKMNPQRSTVWXYZ01"
        )
        XCTAssertEqual(AccountNumber.normalize("oil"), "011")
        XCTAssertEqual(AccountNumber.normalize("  ABCD-EFGH  "), "ABCDEFGH")
    }

    func testNormalizeIsIdempotent() {
        let once = AccountNumber.normalize("abcd-efgh-jkmn-pqrs-tvwx-yz01")
        XCTAssertEqual(AccountNumber.normalize(once), once)
    }

    func testFormatGroupsInBlocksOfFourWithNoTrailingDash() {
        XCTAssertEqual(AccountNumber.format("abcdefghjkmnpqrstvwxyz01"), "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01")
        XCTAssertEqual(AccountNumber.format("abcd"), "ABCD")
        XCTAssertEqual(AccountNumber.format("abcde"), "ABCD-E")
    }

    func testFormatIsStableWhenReappliedToItsOwnOutput() {
        let once = AccountNumber.format("abcdefghjkmnpqrstvwxyz01")
        XCTAssertEqual(AccountNumber.format(once), once)
    }

    func testNothingIsRejectedForLengthOrCharset() {
        XCTAssertEqual(
            AccountNumber.format("abcdefghjkmnpqrstvwxyz0123456"),
            "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345-6"
        )
        XCTAssertEqual(AccountNumber.normalize("ab!cd"), "AB!CD")
    }

    func testOnlyAsciiDigitsCountAsLegacyToken() {
        XCTAssertEqual(AccountNumber.format("١٢٣٤"), "١٢٣٤")
        XCTAssertEqual(AccountNumber.format("١٢٣٤٥٦٧٨"), "١٢٣٤-٥٦٧٨")
    }

    func testEmptyInputStaysEmpty() {
        XCTAssertEqual(AccountNumber.normalize(""), "")
        XCTAssertEqual(AccountNumber.format(""), "")
    }
}
