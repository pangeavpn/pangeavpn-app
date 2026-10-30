import XCTest
@testable import PangeaVPN

final class MainLayoutTests: XCTestCase {
    func testMapTakesTheRoomLeft() {
        XCTAssertEqual(MapSizing.side(viewport: 700, content: 500, width: 362), 200)
    }

    func testMapStopsGrowingAtItsMaximum() {
        XCTAssertEqual(MapSizing.side(viewport: 1000, content: 500, width: 362), MapSizing.maximum)
    }

    // A short screen keeps a small map and scrolls instead of losing it.
    func testMapNeverShrinksBelowItsMinimum() {
        XCTAssertEqual(MapSizing.side(viewport: 600, content: 590, width: 362), MapSizing.minimum)
        XCTAssertEqual(MapSizing.side(viewport: 400, content: 900, width: 362), MapSizing.minimum)
    }

    func testMapFitsTheWidth() {
        XCTAssertEqual(MapSizing.side(viewport: 1000, content: 100, width: 200), 200)
        XCTAssertEqual(MapSizing.side(viewport: 1000, content: 100, width: 80), 80)
        XCTAssertEqual(MapSizing.side(viewport: 1000, content: 100, width: -10), 0)
    }

    func testSpareHeightGoesAroundTheMap() {
        XCTAssertEqual(MapSizing.band(viewport: 1000, content: 500, side: 288), 500)
        XCTAssertEqual(MapSizing.band(viewport: 600, content: 590, side: 112), 112)
    }

    func testPhonesGetTwoByTwoFacts() {
        for width: CGFloat in [295, 335, 362, 400] {
            XCTAssertEqual(FactsLayout.columns(width: width, valueSize: 15), 2, "\(width)")
        }
    }

    func testWideScreensGetOneRowOfFacts() {
        XCTAssertEqual(FactsLayout.columns(width: 640, valueSize: 15), 4)
        let edge = FactsLayout.fourColumnWidth(valueSize: 15)
        XCTAssertEqual(FactsLayout.columns(width: edge, valueSize: 15), 4)
        XCTAssertEqual(FactsLayout.columns(width: edge - 1, valueSize: 15), 2)
    }

    func testLargerTextNeedsMoreWidthForOneRow() {
        XCTAssertGreaterThan(FactsLayout.fourColumnWidth(valueSize: 22), FactsLayout.fourColumnWidth(valueSize: 15))
        XCTAssertEqual(FactsLayout.columns(width: 600, valueSize: 22), 2)
    }

    private let regions = Regions.group([
        Server(id: "nl-ams-1", name: "Amsterdam", region: "eu", country: "NL", load: 30),
        Server(id: "us-nyc-1", name: "", region: "us", country: "US", load: 70),
    ])

    func testRouteNodeComesFromTheServerList() {
        XCTAssertEqual(RouteNode.resolve(id: "nl-ams-1", fallbackName: "ignored", in: regions),
                       RouteNode(name: "Amsterdam", country: "NL"))
        XCTAssertEqual(RouteNode.resolve(id: "us-nyc-1", fallbackName: "", in: regions),
                       RouteNode(name: "us-nyc-1", country: "US"))
    }

    func testRouteNodeFallsBackForAnUnlistedServer() {
        XCTAssertEqual(RouteNode.resolve(id: "jp-tyo-1", fallbackName: "Tokyo", in: regions),
                       RouteNode(name: "Tokyo", country: ""))
        XCTAssertEqual(RouteNode.resolve(id: "jp-tyo-1", fallbackName: "", in: []),
                       RouteNode(name: "jp-tyo-1", country: ""))
    }
}
