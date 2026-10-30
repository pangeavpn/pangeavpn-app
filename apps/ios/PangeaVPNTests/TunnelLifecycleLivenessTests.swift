import XCTest
@testable import PangeaVPN

final class TunnelLifecycleLivenessTests: XCTestCase {
    func testStaleHandshakeReconnectsOnceTheStartGraceIsOver() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.handshake(agedBy: 200)
        rig.ticker.advance(by: 15)
        XCTAssertEqual(rig.clock.pending, [])
        rig.ticker.advance(by: 15)
        XCTAssertEqual(rig.clock.pending, [0])
        XCTAssertEqual(rig.events, [.reconnecting])
        rig.handshake(agedBy: 1)
        rig.clock.advance(by: 0)
        XCTAssertEqual(rig.core.calls, ["networkChanged", "reload", "stop", "start(7)"])
        XCTAssertEqual(rig.events, [.reconnecting, .restored])
        XCTAssertEqual(rig.ticker.pending, [LivenessPolicy.checkInterval])
    }

    func testFreshHandshakeNeverReconnects() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.handshake(agedBy: 100)
        rig.ticker.advance(by: 600)
        XCTAssertEqual(rig.clock.pending, [])
        XCTAssertEqual(rig.core.calls, [])
        XCTAssertEqual(rig.ticker.pending, [LivenessPolicy.checkInterval])
    }

    func testEveryTickLogsAvailableMemory() {
        let rig = LifecycleRig().running(on: "wifi")
        let atStart = rig.host.memoryReads
        rig.ticker.advance(by: 45)
        XCTAssertEqual(rig.host.memoryReads, atStart + 3)
    }

    func testNoHandshakeYetIsNotStale() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.ticker.advance(by: 600)
        XCTAssertEqual(rig.clock.pending, [])
    }

    func testWakeGivesTheTunnelAGraceToHandshakeAgain() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.handshake(agedBy: 10)
        rig.ticker.advance(by: 100)
        rig.handshake(agedBy: 3_600)
        rig.lifecycle.wake()
        rig.ticker.advance(by: 16)
        XCTAssertEqual(rig.clock.pending, [])
        rig.ticker.advance(by: 1)
        XCTAssertEqual(rig.clock.pending, [0])
        XCTAssertEqual(rig.events, [.reconnecting])
    }

    func testWakeChecksFindARecoveredHandshakeHealthy() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.handshake(agedBy: 10)
        rig.ticker.advance(by: 100)
        rig.lifecycle.wake()
        XCTAssertEqual(rig.ticker.pending.sorted(), [5, 17, 32])
        rig.ticker.advance(by: 45)
        XCTAssertEqual(rig.clock.pending, [])
        XCTAssertEqual(rig.events, [])
    }

    func testRecoveryStopsLivenessChecksUntilTheTunnelIsBack() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1_000
        rig.path("cell")
        XCTAssertEqual(rig.ticker.pending, [])
        rig.lifecycle.wake()
        XCTAssertEqual(rig.ticker.pending, [])
    }

    func testStaleCheckRightAfterAReconnectIsInGrace() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.ticker.advance(by: 60)
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        rig.handshake(agedBy: 500)
        rig.ticker.advance(by: 15)
        XCTAssertEqual(rig.clock.pending, [])
        rig.ticker.advance(by: 15)
        XCTAssertEqual(rig.clock.pending, [0])
    }
}
