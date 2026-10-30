import XCTest
@testable import PangeaVPN

final class RotationTests: XCTestCase {
    // As retryOrder builds it: the current server, its region's sibling, then later regions.
    private let order = ["eu-west-2", "eu-west-1", "us-east-1", "ap-southeast-1"]

    func testPlanFollowsTheRetryOrderSoTheSiblingComesFirst() {
        XCTAssertEqual(Rotation.plan(order: order, currentId: "eu-west-2", visited: []).plan,
                       ["eu-west-1", "us-east-1", "ap-southeast-1"])
    }

    func testPlanNeverIncludesTheServerBeingLeft() {
        XCTAssertFalse(Rotation.plan(order: order, currentId: "eu-west-2", visited: []).plan.contains("eu-west-2"))
    }

    func testPlanMovesThisCyclesVisitedServersBehindTheFreshOnes() {
        XCTAssertEqual(Rotation.plan(order: order, currentId: "eu-west-2", visited: ["eu-west-1"]).plan,
                       ["us-east-1", "ap-southeast-1", "eu-west-1"])
    }

    func testPlanStartsOverOnceEveryAlternativeWasVisited() {
        let rotation = Rotation.plan(order: order, currentId: "eu-west-2",
                                     visited: ["eu-west-1", "us-east-1", "ap-southeast-1"])
        XCTAssertEqual(rotation.visited, [])
        XCTAssertEqual(rotation.plan, ["eu-west-1", "us-east-1", "ap-southeast-1"])
    }

    func testPlanDropsIdsNoLongerListedSoAGoneServerCannotBlockTheReset() {
        let rotation = Rotation.plan(order: order, currentId: "eu-west-2", visited: ["gone-1", "eu-west-1"])
        XCTAssertEqual(rotation.visited, ["eu-west-1"])
        XCTAssertEqual(rotation.plan, ["us-east-1", "ap-southeast-1", "eu-west-1"])
    }

    func testPlanIsEmptyWithNowhereElseToGo() {
        XCTAssertEqual(Rotation.plan(order: ["eu-west-1"], currentId: "eu-west-1", visited: []).plan, [])
    }

    func testRecordRemembersTheServerLeftTheFailedAttemptsAndTheLanding() {
        XCTAssertEqual(Rotation.record(visited: [], currentId: "eu-west-2",
                                       plan: ["eu-west-1", "us-east-1", "ap-southeast-1"], landedId: "us-east-1"),
                       ["eu-west-2", "eu-west-1", "us-east-1"])
    }

    func testRecordRemembersEveryAttemptWhenNoneConnected() {
        XCTAssertEqual(Rotation.record(visited: [], currentId: "eu-west-2", plan: ["eu-west-1", "us-east-1"], landedId: nil),
                       ["eu-west-2", "eu-west-1", "us-east-1"])
    }

    func testRecordKeepsEarlierHistoryWithoutRepeats() {
        XCTAssertEqual(Rotation.record(visited: ["us-east-1"], currentId: "eu-west-2",
                                       plan: ["eu-west-1", "us-east-1"], landedId: "us-east-1"),
                       ["us-east-1", "eu-west-2", "eu-west-1"])
    }

    func testRecordTreatsALandingOutsideThePlanAsNoLanding() {
        XCTAssertEqual(Rotation.record(visited: [], currentId: "a", plan: ["b", "c"], landedId: "z"), ["a", "b", "c"])
    }
}
