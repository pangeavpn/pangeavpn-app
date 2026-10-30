import XCTest
@testable import PangeaVPN

final class RegionPickerModelTests: XCTestCase {
    private typealias Model = RegionPickerModel

    private func server(_ id: String, name: String = "Node", country: String = "GB",
                        load: Int? = nil, multihop: Bool = false) -> Server {
        Server(id: id, name: name, region: "eu", country: country, load: load, multihop: multihop)
    }

    private var hops: [Server] {
        [
            server("uk-lon-1", name: "London", load: 40, multihop: true),
            server("uk-lon-2", name: "London", load: 10, multihop: true),
            server("de-fra-1", name: "Frankfurt", country: "DE", load: 30, multihop: true),
            server("nl-ams-1", name: "Amsterdam", country: "NL", load: 30, multihop: true),
            server("us-nyc-1", name: "New York", country: "US", load: 5),
        ]
    }

    // MARK: sections

    func testRecentComesFirstInRecentOrderAndTheRestKeepsHubOrder() {
        let regions = Regions.group([server("eu-1"), server("us-1"), server("ap-1"), server("sa-1")])
        let sections = Model.sections(regions, recentKeys: ["ap", "eu"])
        XCTAssertEqual(sections.recent.map(\.key), ["ap", "eu"])
        XCTAssertEqual(sections.rest.map(\.key), ["us", "sa"])
    }

    func testRecentKeysForRegionsNotOnShowAreSkipped() {
        let regions = Regions.group([server("eu-1"), server("us-1")])
        let sections = Model.sections(regions, recentKeys: ["gone", "us", "us"])
        XCTAssertEqual(sections.recent.map(\.key), ["us"])
        XCTAssertEqual(sections.rest.map(\.key), ["eu"])
    }

    func testWithoutRecentsEveryRegionIsInTheRest() {
        let regions = Regions.group([server("eu-1"), server("us-1")])
        let sections = Model.sections(regions, recentKeys: [])
        XCTAssertTrue(sections.recent.isEmpty)
        XCTAssertEqual(sections.rest.map(\.key), ["eu", "us"])
    }

    // MARK: empty states

    func testNoServersAtAllReadsAsNoServers() {
        XCTAssertEqual(Model.empty(visible: [], all: []), .noServers)
    }

    func testServersHiddenByTheTransportReadAsNoneForTransport() {
        let all = Regions.group([server("eu-1")])
        XCTAssertEqual(Model.empty(visible: [], all: all), .noneForTransport)
    }

    func testVisibleRegionsMeanNoEmptyState() {
        let all = Regions.group([server("eu-1")])
        XCTAssertNil(Model.empty(visible: all, all: all))
    }

    // MARK: names and nodes

    func testRegionNameDropsTheNodeOrdinalAndSkipsUnnamedNodes() {
        let region = Regions.group([server("nl-1", name: ""), server("nl-2", name: "Amsterdam 2")])[0]
        XCTAssertEqual(Model.name(of: region), "Amsterdam")
    }

    func testRegionNameFallsBackToTheKey() {
        let region = Regions.group([server("nl-1", name: "")])[0]
        XCTAssertEqual(Model.name(of: region), "nl")
    }

    func testCountryComesFromTheFirstNodeThatReportsOne() {
        let region = Regions.group([server("nl-1", country: ""), server("nl-2", country: "NL")])[0]
        XCTAssertEqual(Model.country(of: region), "NL")
    }

    func testRowNodePrefersThePinThenTheNodeInUseThenTheLightest() {
        let region = Regions.group([server("eu-1", load: 50), server("eu-2", load: 10), server("eu-3", load: 70)])[0]
        XCTAssertEqual(Model.node(for: region, selectedId: "eu-3", pinnedId: "eu-1").id, "eu-1")
        XCTAssertEqual(Model.node(for: region, selectedId: "eu-3", pinnedId: "us-1").id, "eu-3")
        XCTAssertEqual(Model.node(for: region, selectedId: nil, pinnedId: nil).id, "eu-2")
    }

    func testHoldsSelectionOnlyForTheRegionOwningTheSelectedNode() {
        let regions = Regions.group([server("eu-1"), server("us-1")])
        XCTAssertTrue(Model.holdsSelection(regions[0], selectedId: "eu-1"))
        XCTAssertFalse(Model.holdsSelection(regions[1], selectedId: "eu-1"))
        XCTAssertFalse(Model.holdsSelection(regions[0], selectedId: nil))
    }

    func testNodesAreListedOnlyForRegionsWithTwoOrMore() {
        let regions = Regions.group([server("eu-1"), server("eu-2"), server("us-1")])
        XCTAssertEqual(Model.nodes(of: regions[0], pinnedId: nil).map(\.server.id), ["eu-1", "eu-2"])
        XCTAssertTrue(Model.nodes(of: regions[1], pinnedId: nil).isEmpty)
    }

    func testNodeBadgesMarkThePinOverTheBest() {
        let region = Regions.group([server("eu-1", load: 10), server("eu-2", load: 50), server("eu-3", load: 90)])[0]
        let pinnedBest = Model.nodes(of: region, pinnedId: "eu-1").map(\.badge)
        XCTAssertEqual(pinnedBest, [.pinned, nil, nil])
        let pinnedOther = Model.nodes(of: region, pinnedId: "eu-3").map(\.badge)
        XCTAssertEqual(pinnedOther, [.best, nil, .pinned])
    }

    // MARK: multihop entries

    func testMultihopIsAvailableOnlyWhenSomeServerCanRelay() {
        XCTAssertTrue(Model.multihopAvailable(Regions.group(hops)))
        XCTAssertFalse(Model.multihopAvailable(Regions.group([server("eu-1"), server("us-1")])))
    }

    func testCandidatesSkipTheExitRegionAndNonRelaysAndSortByLoadKeepingHubOrderOnTies() {
        let ids = Model.entryCandidates(hops, exitId: "uk-lon-1").map(\.id)
        XCTAssertEqual(ids, ["de-fra-1", "nl-ams-1"])
    }

    func testUnreportedLoadSortsLast() {
        let list = [server("a-1", multihop: true), server("b-1", load: 99, multihop: true)]
        XCTAssertEqual(Model.entryCandidates(list, exitId: "c-1").map(\.id), ["b-1", "a-1"])
    }

    func testWithNoExitEveryRelayIsACandidate() {
        XCTAssertEqual(Model.entryCandidates(hops, exitId: "").map(\.id), ["uk-lon-2", "de-fra-1", "nl-ams-1", "uk-lon-1"])
    }

    func testAChosenEntryWinsWhileItQualifies() {
        XCTAssertEqual(Model.resolveEntry(hops, exitId: "us-nyc-1", chosenId: "nl-ams-1")?.id, "nl-ams-1")
    }

    func testAChosenEntryInTheExitRegionFallsBackToTheLightest() {
        XCTAssertEqual(Model.resolveEntry(hops, exitId: "uk-lon-1", chosenId: "uk-lon-2")?.id, "de-fra-1")
    }

    func testNoCandidatesResolvesToNil() {
        XCTAssertNil(Model.resolveEntry([server("eu-1")], exitId: "us-1", chosenId: nil))
    }

    func testAutoPanelOffersAutoThenOneChipPerRelayRegionWithTheExitDisabled() {
        let panel = Model.entryPanel(visible: Regions.group(hops), exitId: "de-fra-1", chosenId: nil)
        XCTAssertEqual(panel.chips.map(\.id), [.auto, .region("uk-lon"), .region("de-fra"), .region("nl-ams")])
        XCTAssertEqual(panel.chips.map(\.selected), [true, false, false, false])
        XCTAssertEqual(panel.chips.map(\.isExit), [false, false, true, false])
        XCTAssertEqual(panel.chips[1].serverId, "uk-lon-2")
        XCTAssertNil(panel.chips[0].serverId)
        XCTAssertEqual(panel.chips[1].name, "London")
        XCTAssertEqual(panel.hint, .autoPicks("London"))
    }

    func testAUsableChoiceSelectsItsRegionChipAndHidesTheHint() {
        let panel = Model.entryPanel(visible: Regions.group(hops), exitId: "us-nyc-1", chosenId: "nl-ams-1")
        XCTAssertEqual(panel.chips.filter(\.selected).map(\.id), [.region("nl-ams")])
        XCTAssertEqual(panel.hint, .hidden)
    }

    func testAChoiceThatNoLongerQualifiesFallsBackToAuto() {
        let panel = Model.entryPanel(visible: Regions.group(hops), exitId: "nl-ams-1", chosenId: "nl-ams-1")
        XCTAssertEqual(panel.chips.filter(\.selected).map(\.id), [.auto])
        XCTAssertEqual(panel.hint, .autoPicks("London"))
    }

    func testNoRelayOutsideTheExitRegionSaysSo() {
        let only = [server("uk-lon-1", name: "London", multihop: true)]
        let panel = Model.entryPanel(visible: Regions.group(only), exitId: "uk-lon-1", chosenId: nil)
        XCTAssertEqual(panel.hint, .noEntries)
        XCTAssertEqual(panel.chips.map(\.isExit), [false, true])
    }

    func testTheEntryRegionRowIsMarkedAndLockedOnlyWhenPickedByHand() {
        let regions = Regions.group(hops)
        let london = regions[0]
        let auto = Model.resolveEntry(hops, exitId: "us-nyc-1", chosenId: nil)
        XCTAssertEqual(Model.role(of: london, entry: auto, chosenId: nil, selectedId: "us-nyc-1"), .entry)
        let picked = Model.resolveEntry(hops, exitId: "us-nyc-1", chosenId: "uk-lon-2")
        XCTAssertEqual(Model.role(of: london, entry: picked, chosenId: "uk-lon-2", selectedId: "us-nyc-1"), .lockedEntry)
        XCTAssertEqual(Model.role(of: regions[1], entry: picked, chosenId: "uk-lon-2", selectedId: "us-nyc-1"), .plain)
    }

    func testTheExitRegionIsNeverMarkedAsTheEntry() {
        let london = Regions.group(hops)[0]
        let entry = server("uk-lon-2", multihop: true)
        XCTAssertEqual(Model.role(of: london, entry: entry, chosenId: nil, selectedId: "uk-lon-1"), .plain)
        XCTAssertEqual(Model.role(of: london, entry: nil, chosenId: nil, selectedId: nil), .plain)
    }
}
