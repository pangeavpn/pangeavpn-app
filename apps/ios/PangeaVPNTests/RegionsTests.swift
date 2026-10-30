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

    func testSelectionRestoresTheLastServerWithNoPickAndNothingFromAnEmptyList() {
        XCTAssertEqual(Regions.resolveSelection(visible: [server("a"), server("c")], previous: "", lastServerId: "c"), "c")
        XCTAssertEqual(Regions.resolveSelection(visible: [], previous: "a", lastServerId: "b"), "")
    }

    func testKeyLeavesIdsWithoutANumericSuffixAlone() {
        XCTAssertEqual(Regions.key(ofId: "eu-central-12"), "eu-central")
        XCTAssertEqual(Regions.key(ofId: "amsterdam"), "amsterdam")
        XCTAssertEqual(Regions.key(ofId: "eu-west-a"), "eu-west-a")
    }

    func testSupportingKeepsOnlyCapableNodesAndDropsEmptyRegions() {
        let reality = Server.universalTransports.union(["reality"])
        let regions = Regions.group([
            Server(id: "eu-1", name: "A", region: "eu", country: "DE", load: nil),
            Server(id: "eu-2", name: "A", region: "eu", country: "DE", load: nil, transports: reality),
            Server(id: "us-1", name: "B", region: "us", country: "US", load: nil),
        ])
        let filtered = Regions.supporting("reality", in: regions)
        XCTAssertEqual(filtered.map(\.key), ["eu"])
        XCTAssertEqual(filtered.first?.servers.map(\.id), ["eu-2"])
        XCTAssertEqual(Regions.supporting("auto", in: regions), regions)
    }

    func testOrderByRecentPutsRecentsFirstAndIgnoresGoneOnes() {
        let regions = Regions.group([server("eu-1"), server("uk-1"), server("us-1")])
        XCTAssertEqual(Regions.orderByRecent(regions, recent: ["us", "uk"]).map(\.key), ["us", "uk", "eu"])
        XCTAssertEqual(Regions.orderByRecent(regions, recent: ["gone", "uk"]).map(\.key), ["uk", "eu", "us"])
    }

    func testPromoteRecentMovesToTheFrontWithoutDuplicatesAndCaps() {
        XCTAssertEqual(Regions.promoteRecent(["a", "b", "c"], key: "c"), ["c", "a", "b"])
        XCTAssertEqual(Regions.promoteRecent(["a", "b"], key: "z"), ["z", "a", "b"])
        XCTAssertEqual(Regions.promoteRecent(["a", "b", "c"], key: "z", limit: 2), ["z", "a"])
        XCTAssertEqual(Regions.promoteRecent((1...8).map(String.init), key: "z").count, SharedPrefs.maxRecentRegions)
    }

    func testRetryOrderExhaustsTheSelectedRegionBeforeLaterOnes() {
        let servers = [server("eu-central-1", load: 70), server("us-east-1", load: 5), server("eu-central-2", load: 20),
                       server("eu-central-3"), server("us-east-2", load: 10), server("ap-south-1", load: 1)]
        XCTAssertEqual(Regions.retryOrder(from: "eu-central-1", in: servers),
                       ["eu-central-1", "eu-central-2", "eu-central-3", "us-east-1", "us-east-2", "ap-south-1"])
    }

    func testRetryOrderCascadesThroughEveryServerWhenTheChosenOneIsGone() {
        let servers = [server("eu-west-1", load: 20), server("eu-west-2", load: 5), server("us-east-1", load: 10)]
        XCTAssertEqual(Regions.retryOrder(from: "gone-1", in: servers), ["eu-west-2", "eu-west-1", "us-east-1"])
        XCTAssertEqual(Regions.retryOrder(from: "gone-1", in: []), [])
    }

    func testRetryOrderContinuesWithTheNextRegionAndWrapsAround() {
        let servers = [server("eu-west-1", load: 20), server("us-east-1", load: 30), server("ap-south-1", load: 40)]
        XCTAssertEqual(Regions.retryOrder(from: "us-east-1", in: servers), ["us-east-1", "ap-south-1", "eu-west-1"])
    }

    func testRetryOrderKeepsHubOrderBetweenEqualLoads() {
        let servers = [server("eu-1"), server("eu-2"), server("eu-3", load: 50)]
        XCTAssertEqual(Regions.retryOrder(from: "eu-3", in: servers), ["eu-3", "eu-1", "eu-2"])
    }

    func testCandidatesTakeAllSiblingsThenOneNodePerRegionUpToTheCap() {
        let servers = [server("eu-1", load: 9), server("eu-2", load: 1), server("us-1", load: 30), server("us-2", load: 3),
                       server("ap-1"), server("sa-1"), server("af-1"), server("oc-1")]
        XCTAssertEqual(Regions.candidates(from: "eu-1", in: servers), ["eu-1", "eu-2", "us-2", "ap-1", "sa-1", "af-1"])
        XCTAssertEqual(Regions.candidates(from: "eu-1", in: servers, limit: 3), ["eu-1", "eu-2", "us-2"])
        XCTAssertEqual(Regions.candidateLimit, 6)
    }

    func testCandidatesAlwaysLeadWithTheChosenServer() {
        XCTAssertEqual(Regions.candidates(from: "gone-1", in: [server("eu-1"), server("us-1")]), ["gone-1", "eu-1", "us-1"])
        XCTAssertEqual(Regions.candidates(from: "solo-1", in: []), ["solo-1"])
    }

    func testSubtitleForTheCurrentRegion() {
        let region = Regions.group([server("eu-1", load: 40), server("eu-2", load: 10)])[0]
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: "eu-1", pinnedId: "eu-1"), "eu-1 · pinned")
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: "eu-2", pinnedId: nil), "eu-2 · best of 2")
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: "eu-2", pinnedId: "us-1"), "eu-2 · best of 2")
    }

    func testSubtitleForOtherRegions() {
        let region = Regions.group([server("eu-1", load: 40), server("eu-2", load: 10), server("eu-3")])[0]
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: "us-1", pinnedId: nil), "3 servers · auto")
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: nil, pinnedId: nil), "3 servers · auto")
    }

    func testSubtitleForASingleNodeRegionIsTheNodeId() {
        let region = Regions.group([server("sg-1")])[0]
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: nil, pinnedId: nil), "sg-1")
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: "sg-1", pinnedId: nil), "sg-1")
        XCTAssertEqual(Regions.subtitle(for: region, selectedId: "sg-1", pinnedId: "sg-1"), "sg-1 · pinned")
    }
}
