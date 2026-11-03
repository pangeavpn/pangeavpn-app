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

    // A 17 Pro leaves the stack about 640 pt; banner, two-line hero, facts and slots take about 555.
    func testTheFullestStateFitsAnIPhone17ProWithoutScrolling() {
        let viewport: CGFloat = 640
        let content: CGFloat = 555
        let side = MapSizing.side(viewport: viewport, content: content, width: 362)
        XCTAssertLessThanOrEqual(content + MapSizing.band(viewport: viewport, content: content, side: side), viewport)
    }

    func testSpareHeightGoesAroundTheMap() {
        XCTAssertEqual(MapSizing.band(viewport: 1000, content: 500, side: 288), 500)
        XCTAssertEqual(MapSizing.band(viewport: 600, content: 590, side: 112), 112)
    }

    private let slotMetrics = SlotMetrics(label: 14, row: 56, door: 48)

    func testIdleRowsWaitForMeasurements() {
        XCTAssertEqual(SlotBudget.idleRows(viewport: 900, fixed: 150, metrics: SlotMetrics()), Regions.slotCount)
    }

    // Sizes are the stack heights an SE, a 17 Pro and a 17 Pro Max leave below the header.
    func testIdleRowsGrowWithTheScreen() {
        XCTAssertEqual(SlotBudget.idleRows(viewport: 505, fixed: 146, metrics: slotMetrics), 3)
        XCTAssertEqual(SlotBudget.idleRows(viewport: 636, fixed: 146, metrics: slotMetrics), 4)
        XCTAssertEqual(SlotBudget.idleRows(viewport: 718, fixed: 146, metrics: slotMetrics), 5)
    }

    func testIdleRowsStopAtTheMaximum() {
        XCTAssertEqual(SlotBudget.idleRows(viewport: 2000, fixed: 146, metrics: slotMetrics), SlotBudget.idleMaximum)
    }

    // One extra row is still worth a small map; two slots stay when even that would scroll.
    func testIdleRowsFallBackToTheMinimumMap() {
        let tight = 146 + slotMetrics.chrome + 3 * slotMetrics.pitch + MapSizing.minimum
        XCTAssertEqual(SlotBudget.idleRows(viewport: tight, fixed: 146, metrics: slotMetrics), 3)
        XCTAssertEqual(SlotBudget.idleRows(viewport: tight - 1, fixed: 146, metrics: slotMetrics), Regions.slotCount)
    }

    func testIdleRowsKeepTheMapComfortable() {
        for viewport in stride(from: CGFloat(500), through: 900, by: 7) {
            let rows = SlotBudget.idleRows(viewport: viewport, fixed: 146, metrics: slotMetrics)
            guard rows > Regions.slotCount + 1 else { continue }
            let map = viewport - 146 - slotMetrics.chrome - CGFloat(rows) * slotMetrics.pitch
            XCTAssertGreaterThanOrEqual(map, SlotBudget.comfortableMap, "\(viewport)")
        }
    }

    func testWideMapTakesAboutTwoFifthsOfTheStage() {
        XCTAssertEqual(MapSizing.wideColumn(width: 700), 280)
        XCTAssertEqual(MapSizing.wideColumn(width: 1000), 400)
    }

    func testWideMapColumnStopsAtItsMaximum() {
        XCTAssertEqual(MapSizing.wideColumn(width: 1376), MapSizing.wideColumnMaximum)
        XCTAssertEqual(MapSizing.wideColumn(width: -10), 0)
    }

    // Desktop draws its 336 pt map in a 268 pt column.
    func testWideMapOverhangsItsColumnAsOnDesktop() {
        XCTAssertEqual(MapSizing.wideSide(column: 268, height: 1000), 335)
    }

    func testWideMapShrinksToAShortStage() {
        XCTAssertEqual(MapSizing.wideSide(column: 400, height: 420), 420)
        XCTAssertEqual(MapSizing.wideSide(column: 400, height: -5), 0)
    }

    func testWideRowsWaitForMeasurements() {
        XCTAssertEqual(SlotBudget.wideRows(viewport: 900, fixed: 106, metrics: SlotMetrics()), Regions.slotCount)
    }

    // Heights are what the capped hero column leaves on an iPad mini, an 11-inch and a 13-inch iPad in landscape.
    func testWideRowsFillTheColumn() {
        XCTAssertEqual(SlotBudget.wideRows(viewport: 530, fixed: 106, metrics: slotMetrics), 5)
        XCTAssertEqual(SlotBudget.wideRows(viewport: 620, fixed: 106, metrics: slotMetrics), 6)
        XCTAssertEqual(SlotBudget.wideRows(viewport: 734, fixed: 106, metrics: slotMetrics), 8)
    }

    func testWideRowsStopAtTheMaximum() {
        XCTAssertEqual(SlotBudget.wideRows(viewport: 3000, fixed: 106, metrics: slotMetrics), SlotBudget.wideMaximum)
    }

    // A column too short for even two rows keeps them and scrolls.
    func testWideRowsKeepTheTwoSlots() {
        XCTAssertEqual(SlotBudget.wideRows(viewport: 200, fixed: 106, metrics: slotMetrics), Regions.slotCount)
    }

    func testWideFactsTakeBackTheRowsTheyCover() {
        let idle = SlotBudget.wideRows(viewport: 734, fixed: 106, metrics: slotMetrics)
        let busy = SlotBudget.wideRows(viewport: 734, fixed: 106 + 70, metrics: slotMetrics)
        XCTAssertEqual(idle, 8)
        XCTAssertEqual(busy, 7)
    }

    func testWideRowsNeverOverfillTheColumn() {
        for viewport in stride(from: CGFloat(300), through: 1400, by: 7) {
            let rows = SlotBudget.wideRows(viewport: viewport, fixed: 106, metrics: slotMetrics)
            guard rows > Regions.slotCount else { continue }
            let used = 106 + slotMetrics.chrome + CGFloat(rows) * slotMetrics.pitch
            XCTAssertLessThanOrEqual(used, viewport, "\(viewport)")
        }
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
