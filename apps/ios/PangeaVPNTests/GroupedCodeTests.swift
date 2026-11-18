import XCTest
@testable import PangeaVPN

final class GroupedCodeTests: XCTestCase {
    func testAccountNumberBreaksIntoEvenLines() {
        XCTAssertEqual(GroupedCode.layouts("ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"), [
            "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01",
            "ABCD-EFGH-JKMN-\nPQRS-TVWX-YZ01",
            "ABCD-EFGH-\nJKMN-PQRS-\nTVWX-YZ01",
            "ABCD-\nEFGH-\nJKMN-\nPQRS-\nTVWX-\nYZ01",
        ])
    }

    func testUnevenCountKeepsTheShortLineLast() {
        XCTAssertEqual(GroupedCode.layouts("AA-BB-CC"), ["AA-BB-CC", "AA-BB-\nCC", "AA-\nBB-\nCC"])
    }

    func testTextWithoutGroupsIsLeftAlone() {
        XCTAssertEqual(GroupedCode.layouts("Account number"), ["Account number"])
    }
}
