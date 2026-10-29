import SwiftUI
import XCTest
@testable import PangeaVPN

final class HeroTextTests: XCTestCase {
    private struct Piece: Equatable {
        let text: String
        let strong: Bool
        let color: Color?
    }

    private func pieces(_ attributed: AttributedString) -> [Piece] {
        attributed.runs.map { run in
            Piece(
                text: String(attributed[run.range].characters),
                strong: run.inlinePresentationIntent == .stronglyEmphasized,
                color: run.foregroundColor
            )
        }
    }

    func testEmphasisInMiddle() {
        let result = pieces(HeroText.emphasised("The %1$@ is here", emphasis: "world"))
        XCTAssertEqual(result, [
            Piece(text: "The ", strong: false, color: nil),
            Piece(text: "world", strong: true, color: Palette.accent),
            Piece(text: " is here", strong: false, color: nil),
        ])
    }

    func testEmphasisAtEnd() {
        let result = pieces(HeroText.emphasised("The world is %1$@", emphasis: "whole"))
        XCTAssertEqual(result, [
            Piece(text: "The world is ", strong: false, color: nil),
            Piece(text: "whole", strong: true, color: Palette.accent),
        ])
    }

    func testEmphasisAtStart() {
        let result = pieces(HeroText.emphasised("%1$@ again", emphasis: "Once"))
        XCTAssertEqual(result, [
            Piece(text: "Once", strong: true, color: Palette.accent),
            Piece(text: " again", strong: false, color: nil),
        ])
    }

    func testMissingSlotIsPlainText() {
        let attributed = HeroText.emphasised("No slot here", emphasis: "ignored")
        XCTAssertEqual(pieces(attributed), [Piece(text: "No slot here", strong: false, color: nil)])
    }

    func testEmptyEmphasisDropsSlot() {
        let attributed = HeroText.emphasised("Couldn't %1$@!", emphasis: "")
        XCTAssertEqual(String(attributed.characters), "Couldn't !")
        XCTAssertTrue(pieces(attributed).allSatisfy { !$0.strong && $0.color == nil })
    }

    func testAccentForEveryState() {
        XCTAssertEqual(HeroText.accent(.connected), Palette.connected)
        XCTAssertEqual(HeroText.accent(.connecting), Palette.connecting)
        XCTAssertEqual(HeroText.accent(.disconnecting), Palette.connecting)
        XCTAssertEqual(HeroText.accent(.error), Palette.error)
        XCTAssertEqual(HeroText.accent(.disconnected), Palette.textSecondary)
    }
}
