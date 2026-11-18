import XCTest
@testable import PangeaVPN

final class LineBreaksTests: XCTestCase {
    func testSeparatorStaysAtTheEndOfALine() {
        XCTAssertEqual(LineBreaks.separators("2 servers · auto"), "2 servers\u{00A0}· auto")
        XCTAssertEqual(LineBreaks.separators("Direct IP · REALITY · Relay"), "Direct IP\u{00A0}· REALITY\u{00A0}· Relay")
    }

    func testIdsNeverBreakAtTheirHyphens() {
        XCTAssertEqual(LineBreaks.ids("eu-central-2"), "eu-\u{2060}central-\u{2060}2")
    }

    func testUnbrokenGluesEveryWord() {
        XCTAssertEqual(LineBreaks.unbroken("Oct 20, 2026"), "Oct\u{00A0}20,\u{00A0}2026")
    }

    func testLeadIsTheTextBeforeTheFirstSeparator() {
        XCTAssertEqual(LineBreaks.lead("eu-london-1 · best of 2"), "eu-london-1")
        XCTAssertEqual(LineBreaks.lead("sg-1"), "sg-1")
    }
}
