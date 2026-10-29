import XCTest
@testable import PangeaVPN

final class RegionsTests: XCTestCase {
    private func server(_ id: String, load: Int? = nil) -> Server {
        Server(id: id, name: "Node", region: "eu", country: "GB", load: load)
    }

    func testGroupKeyStripsTheNumericNodeSuffix() {
        let regions = Regions.group([server("eu-central-1"), server("eu-central-12"), server("standalone")])
        XCTAssertEqual(regions.map(\.key), ["eu-central", "standalone"])
        XCTAssertEqual(regions.first?.servers.count, 2)
    }

    func testGroupingPreservesFirstSeenOrderAndCollectsNodes() {
        let regions = Regions.group([server("eu-1"), server("us-1"), server("eu-2")])
        XCTAssertEqual(regions.map(\.key), ["eu", "us"])
        XCTAssertEqual(regions.first?.servers.map(\.id), ["eu-1", "eu-2"])
    }

    func testANodeThatReportsLoadBeatsOneThatDoesNot() {
        let region = Regions.group([server("eu-1", load: nil), server("eu-2", load: 90)])[0]
        XCTAssertEqual(region.best.id, "eu-2")
    }

    func testRegionPicksItsLowestLoadNode() {
        let region = Regions.group([server("eu-1", load: 80), server("eu-2", load: 10), server("eu-3", load: 40)])[0]
        XCTAssertEqual(region.best.id, "eu-2")
        XCTAssertEqual(region.best.load, 10)
    }

    func testBestIsFirstWhenNoNodeReportsLoad() {
        let region = Regions.group([server("eu-1"), server("eu-2")])[0]
        XCTAssertEqual(region.best.id, "eu-1")
    }

    func testEmptyInputGivesNoRegions() {
        XCTAssertTrue(Regions.group([]).isEmpty)
    }

    private var threeRegions: [Region] {
        Regions.group([server("eu-1"), server("us-1"), server("ap-1")])
    }

    func testRegionOfServerFindsTheOwningRegion() {
        let regions = Regions.group([server("eu-1"), server("us-1")])
        XCTAssertEqual(Regions.region(of: "us-1", in: regions)?.key, "us")
        XCTAssertNil(Regions.region(of: "nope", in: regions))
    }

    func testSlotsTakeTheLeadingRegionsWhenNothingIsSelected() {
        XCTAssertEqual(Regions.slots(threeRegions, selectedKey: nil).map(\.key), ["eu", "us"])
    }

    func testSlotsLeaveTheLeadingRegionsAloneWhenTheSelectionIsAlreadyThere() {
        XCTAssertEqual(Regions.slots(threeRegions, selectedKey: "us").map(\.key), ["eu", "us"])
    }

    func testASelectionOutsideTheLeadingRegionsIsPulledIntoTheFirstSlot() {
        XCTAssertEqual(Regions.slots(threeRegions, selectedKey: "ap").map(\.key), ["ap", "eu"])
    }

    func testASelectionThatNoLongerExistsDoesNotDisturbTheSlots() {
        XCTAssertEqual(Regions.slots(threeRegions, selectedKey: "gone").map(\.key), ["eu", "us"])
    }

    func testSlotCountIsTwoAndCanBeOverridden() {
        XCTAssertEqual(Regions.slotCount, 2)
        XCTAssertEqual(Regions.slots(threeRegions, selectedKey: "ap", count: 1).map(\.key), ["ap"])
    }

    func testSelectionKeepsTheInSessionPickWhileItIsListed() {
        let visible = [server("a"), server("b")]
        XCTAssertEqual(Regions.resolveSelection(visible: visible, previous: "b", lastServerId: "a"), "b")
    }

    func testSelectionFallsBackToTheLastConnectedServer() {
        let visible = [server("a"), server("b")]
        XCTAssertEqual(Regions.resolveSelection(visible: visible, previous: "gone", lastServerId: "a"), "a")
    }

    func testSelectionEmptiesWhenNeitherIsListed() {
        let visible = [server("a")]
        XCTAssertEqual(Regions.resolveSelection(visible: visible, previous: "gone", lastServerId: "also-gone"), "")
        XCTAssertEqual(Regions.resolveSelection(visible: visible, previous: "", lastServerId: nil), "")
    }
}
