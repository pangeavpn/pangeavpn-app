import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelLifecycleSwitchTests: XCTestCase {
    func testSwitchWhileRunningStaysInTheSession() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.switchTo("b", candidates: ["b", "c"])
        XCTAssertEqual(rig.core.calls, ["prepare(b)", "stopForSwitch", "start(7)"])
        XCTAssertEqual(rig.host.reassertingHistory, [true, false])
        XCTAssertEqual(rig.host.applyCount, 0)
        XCTAssertEqual(rig.events, [])
        XCTAssertEqual(rig.clock.pending, [])
        XCTAssertEqual(rig.ticker.pending, [LivenessPolicy.checkInterval])
    }

    func testSwitchReappliesSettingsOnlyWhenTheyChanged() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.tunnel = LifecycleCoreFake.tunnelConfig(dns: ["10.66.0.1"], mtu: 1280)
        rig.switchTo("b")
        XCTAssertEqual(rig.host.applyCount, 1)
        XCTAssertEqual(rig.host.lastSettings?.mtu, NSNumber(value: 1280))
        XCTAssertEqual(rig.core.calls, ["prepare(b)", "stopForSwitch", "start(7)"])
    }

    func testSwitchUsesTheMultihopRoute() {
        let rig = LifecycleRig()
        rig.useMultihop(entry: "e9")
        _ = rig.running(on: "wifi")
        rig.switchTo("b")
        XCTAssertEqual(rig.core.calls, ["prepareVia(b,e9)", "stopForSwitch", "start(7)"])
    }

    func testFailedSwitchPrepareFallsBackThroughNewThenOldServer() {
        let rig = LifecycleRig()
        rig.start(server: "a", candidates: ["c"])
        _ = rig.settled(on: "wifi")
        rig.core.prepareErrors["b"] = LifecycleCoreFake.failure
        rig.switchTo("b", candidates: ["b"])
        XCTAssertEqual(rig.core.calls, ["prepare(b)"])
        XCTAssertTrue(rig.host.reasserting)
        XCTAssertEqual(rig.events, [.reconnecting])
        XCTAssertEqual(rig.clock.pending, [3])
        rig.clock.advance(by: 3 + 6)
        XCTAssertEqual(rig.prepares(), ["prepare(b)", "prepare(b)", "prepare(b)"])
        XCTAssertEqual(rig.clock.pending, [9])
        rig.clock.advance(by: 9)
        XCTAssertEqual(rig.prepares(), ["prepare(b)", "prepare(b)", "prepare(b)", "prepare(a)"])
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.events, [.reconnecting, .restored])
    }

    func testFailedSwitchStartFallsIntoTheReconnectLoop() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1
        rig.switchTo("b")
        XCTAssertEqual(rig.core.calls, ["prepare(b)", "stopForSwitch", "start(7)"])
        XCTAssertEqual(rig.clock.pending, [3])
        rig.core.clearCalls()
        rig.clock.advance(by: 3)
        XCTAssertEqual(rig.core.calls, ["reload", "stop", "prepare(b)", "start(7)"])
        XCTAssertFalse(rig.host.reasserting)
    }

    func testSwitchDuringTheBlockingApplyReplacesThePendingStart() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start(server: "a", candidates: [])
        rig.switchTo("b")
        rig.host.releaseApply()
        rig.host.releaseApply()
        XCTAssertEqual(rig.core.calls, ["reload", "restore", "config", "prepare(b)", "start(7)"])
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.events, [.connected])
    }

    func testSwitchDuringTheSettingsApplyReplacesThePendingStart() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start(server: "a", candidates: [])
        rig.host.releaseApply()
        rig.switchTo("b")
        rig.host.releaseApply()
        rig.clock.advance(by: 10)
        XCTAssertEqual(rig.core.calls, [
            "reload", "restore", "config", "prepare(a)", "reload", "prepare(b)", "start(7)",
        ])
        XCTAssertEqual(rig.outcomes, [nil])
    }

    func testStartReplacedBySwitchFallsBackToTheOriginalServer() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.core.prepareErrors["b"] = LifecycleCoreFake.failure
        rig.start(server: "a", candidates: [])
        rig.switchTo("b")
        rig.host.releaseApply()
        rig.host.releaseApply()
        XCTAssertEqual(rig.prepares(), ["prepare(b)", "prepare(a)"])
        XCTAssertEqual(rig.outcomes, [nil])
    }

    func testSwitchWhileWaitingRunsWhenTheNetworkReturns() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.clock.advance(by: 10)
        rig.path("", usable: false)
        rig.core.clearCalls()
        rig.switchTo("b")
        XCTAssertEqual(rig.core.calls, [])
        rig.path("wifi")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, ["networkChanged", "reload", "stop", "prepare(b)", "start(7)"])
        XCTAssertFalse(rig.host.reasserting)
    }

    func testSwitchDuringBackoffReplacesTheRetry() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 2
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.clock.pending, [3])
        rig.core.clearCalls()
        rig.switchTo("b")
        XCTAssertEqual(rig.core.calls, ["prepare(b)", "stopForSwitch", "start(7)"])
        XCTAssertEqual(rig.clock.pending, [])
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.events, [.reconnecting, .restored])
    }

    func testSwitchIsIgnoredOnceStopped() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.lifecycle.stop()
        rig.core.clearCalls()
        rig.switchTo("b")
        rig.clock.advance(by: 60)
        XCTAssertEqual(rig.core.calls, [])
    }
}
