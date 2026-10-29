import SwiftUI
import XCTest
@testable import PangeaVPN

final class SVGPathTests: XCTestCase {
    func testParsesMoveLineClose() {
        let path = SVGPath.parse("M 0 0 L 10 0 L 10 10 Z")
        XCTAssertEqual(path.boundingRect, CGRect(x: 0, y: 0, width: 10, height: 10))
    }

    func testIgnoresGarbageTokens() {
        let path = SVGPath.parse("M 0 0 bogus L 10 0 ?? L 10 10 Q 99 99 Z")
        XCTAssertEqual(path.boundingRect, CGRect(x: 0, y: 0, width: 10, height: 10))
    }

    func testEmptyInputGivesEmptyPath() {
        XCTAssertTrue(SVGPath.parse("").isEmpty)
    }

    func testEveryDriftPathParsesInsideViewport() {
        let viewport = CGRect(x: 0, y: 0, width: 800, height: 800)
        XCTAssertEqual(DriftPaths.all.count, 3)
        for d in DriftPaths.all {
            let bounds = SVGPath.parse(d).boundingRect
            XCTAssertFalse(bounds.isEmpty)
            XCTAssertTrue(viewport.contains(bounds))
        }
    }
}
