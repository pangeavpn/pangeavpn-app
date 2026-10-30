import XCTest
@testable import PangeaVPN

final class ServerPickTests: XCTestCase {
    private func node(_ id: String, _ load: Int?) -> Server {
        Server(id: id, name: id, region: "eu", country: "GB", load: load)
    }

    // Lands on element `index` of whichever pool the pick settles on.
    private func roll(_ index: Int, of size: Int) -> () -> Double {
        { Double(index) / Double(size) }
    }

    func testNothingToPickFromGivesNil() {
        XCTAssertNil(ServerPick.random(from: []))
    }

    func testTheOnlyServerIsPicked() {
        XCTAssertEqual(ServerPick.random(from: [node("a", 10)], roll: roll(0, of: 1))?.id, "a")
    }

    func testOnlyServersAtOrBelowTheThresholdArePicked() {
        let servers = [node("busy", 90), node("light", 10), node("alsoBusy", 99)]
        for step in 0..<10 {
            XCTAssertEqual(ServerPick.random(from: servers, roll: { Double(step) / 10 })?.id, "light")
        }
    }

    func testTheThresholdIsInclusive() {
        let servers = [node("busy", 76), node("edge", ServerPick.preferredMaxLoad)]
        XCTAssertEqual(ServerPick.random(from: servers, roll: { 0 })?.id, "edge")
    }

    func testEveryServerIsEligibleWhenAllAreCongestedOrUnreported() {
        for servers in [[node("a", 90), node("b", 99)], [node("a", nil), node("b", nil)]] {
            XCTAssertEqual(ServerPick.random(from: servers, roll: roll(0, of: 2))?.id, "a")
            XCTAssertEqual(ServerPick.random(from: servers, roll: roll(1, of: 2))?.id, "b")
        }
    }

    func testUnknownLoadIsNotTreatedAsLight() {
        let servers = [node("unknown", nil), node("light", 5)]
        for step in 0..<10 {
            XCTAssertEqual(ServerPick.random(from: servers, roll: { Double(step) / 10 })?.id, "light")
        }
    }

    func testThePickIsUniformAcrossTheEligiblePool() {
        let servers = [node("a", 10), node("busy", 80), node("b", 20), node("c", 30)]
        let picks = (0..<300).compactMap { step in ServerPick.random(from: servers, roll: { Double(step) / 300 })?.id }
        XCTAssertEqual(Dictionary(grouping: picks, by: { $0 }).mapValues(\.count), ["a": 100, "b": 100, "c": 100])
    }

    func testRollsAtOrPastTheTopStillPickAServer() {
        let servers = [node("a", 10), node("b", 20)]
        XCTAssertEqual(ServerPick.random(from: servers, roll: { 0.999_999_999 })?.id, "b")
        XCTAssertEqual(ServerPick.random(from: servers, roll: { 1 })?.id, "b")
        XCTAssertEqual(ServerPick.random(from: servers, roll: { .nan })?.id, "a")
    }
}
