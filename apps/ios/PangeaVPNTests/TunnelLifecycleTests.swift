import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelLifecycleTests: XCTestCase {
    func testStartReloadsThenRunsTheFullSequence() {
        let rig = LifecycleRig()
        rig.start()
        XCTAssertEqual(rig.core.calls, ["reload", "restore", "config", "prepare(srv)", "start(7)"])
        XCTAssertEqual(rig.host.applyCount, 2)
        XCTAssertEqual(rig.host.lastSettings?.ipv4Settings?.addresses, ["10.66.0.2"])
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.events, [.connected])
        XCTAssertTrue(rig.host.monitoring)
        XCTAssertFalse(rig.host.reasserting)
    }

    func testAppStartWithoutNetworkFailsOnceWithMappedErrorAndStopsMonitoring() {
        let rig = LifecycleRig()
        rig.core.restoreError = CoreError(message: "hub unreachable: every connection method failed")
        rig.start()
        rig.clock.advance(by: 60)
        XCTAssertEqual(rig.outcomes, [TunnelStartError.connectFailed.rawValue])
        XCTAssertEqual(rig.messages, [TunnelStartError.connectFailed.errorDescription])
        XCTAssertFalse(rig.host.monitoring)
        XCTAssertEqual(rig.host.applyCount, 1)
        XCTAssertEqual(rig.clock.pending, [])
    }

    func testSignedOutAppStartMapsToSignedOut() {
        let rig = LifecycleRig()
        rig.core.restoreError = CoreError(message: "no stored session")
        rig.start()
        XCTAssertEqual(rig.outcomes, [TunnelStartError.signedOut.rawValue])
    }

    func testSettingsToggleStartUsesLastServer() {
        let rig = LifecycleRig()
        rig.start(options: nil)
        XCTAssertTrue(rig.core.calls.contains("prepare(last)"))
        XCTAssertEqual(rig.outcomes, [nil])
    }

    func testConfigWithoutUsableDNSFailsAnAppStart() {
        let rig = LifecycleRig()
        rig.core.tunnel = LifecycleCoreFake.tunnelConfig(dns: ["127.0.0.1"])
        rig.start()
        XCTAssertEqual(rig.outcomes, [TunnelStartError.invalidDNS.rawValue])
        XCTAssertEqual(rig.host.applyCount, 1)
    }

    func testSavedProtocolDecidesAllowLan() {
        let rig = LifecycleRig()
        rig.start(excludeLocal: true)
        XCTAssertFalse((rig.host.lastSettings?.ipv4Settings?.excludedRoutes ?? []).isEmpty)
        let other = LifecycleRig()
        other.core.settings.allowLan = true
        other.start(excludeLocal: false)
        XCTAssertTrue((other.host.lastSettings?.ipv4Settings?.excludedRoutes ?? []).isEmpty)
    }

    func testStopDuringSettingsApplyCompletesTheStartOnce() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.host.releaseApply()
        rig.lifecycle.stop()
        XCTAssertEqual(rig.outcomes, [TunnelStartError.stopped.rawValue])
        rig.host.releaseApply()
        rig.clock.advance(by: 10)
        XCTAssertEqual(rig.outcomes.count, 1)
        XCTAssertFalse(rig.core.calls.contains("start(7)"))
        XCTAssertFalse(rig.host.monitoring)
        XCTAssertEqual(rig.events, [.stopped])
    }

    func testStopDuringTheBlockingApplyNeverReachesTheHub() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.lifecycle.stop()
        rig.host.releaseApply()
        rig.clock.advance(by: 10)
        XCTAssertEqual(rig.outcomes, [TunnelStartError.stopped.rawValue])
        XCTAssertEqual(rig.core.calls, ["stop"])
    }

    func testSettingsApplyThatNeverCallsBackProceedsToTheFdProbe() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.host.releaseApply()
        rig.clock.advance(by: 4.9)
        XCTAssertTrue(rig.outcomes.isEmpty)
        XCTAssertFalse(rig.core.calls.contains("start(7)"))
        rig.clock.advance(by: 0.2)
        XCTAssertEqual(rig.outcomes, [nil])
        rig.host.releaseApply(NSError(domain: "NEVPNErrorDomain", code: 1))
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.core.calls.filter { $0 == "start(7)" }.count, 1)
    }

    func testUnansweredBlockingApplyStillContinuesTheStart() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.clock.advance(by: 5.1)
        XCTAssertEqual(rig.core.calls, ["reload", "restore", "config", "prepare(srv)"])
        XCTAssertEqual(rig.host.applyCount, 2)
    }

    func testSettingsTimeoutWithoutAnInterfaceStillFailsAsNoTun() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.host.fd = -1
        rig.start()
        rig.host.releaseApply()
        rig.clock.advance(by: 5.1)
        XCTAssertEqual(rig.outcomes, [TunnelStartError.noTunnelInterface.rawValue])
        rig.host.releaseApply()
        XCTAssertEqual(rig.outcomes.count, 1)
    }

    func testSettingsErrorBeforeTheTimeoutFailsTheStep() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.host.releaseApply()
        rig.clock.advance(by: 1)
        rig.host.releaseApply(NSError(domain: "NEVPNErrorDomain", code: 1))
        rig.clock.advance(by: 10)
        XCTAssertEqual(rig.outcomes, [TunnelStart.otherErrorCode])
        XCTAssertFalse(rig.core.calls.contains("start(7)"))
    }

    func testReloadFailureDoesNotFailTheStart() {
        let rig = LifecycleRig()
        rig.core.reloadFailures = 1
        rig.start()
        XCTAssertEqual(rig.core.calls, ["reload", "restore", "config", "prepare(srv)", "start(7)"])
        XCTAssertEqual(rig.outcomes, [nil])
    }

    func testReloadFailureDoesNotBurnTheReconnectAttempt() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.reloadFailures = 1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, ["networkChanged", "reload", "stop", "start(7)"])
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertNil(rig.note.value)
    }

    func testNetworkChangeMidStartReconnectsOnceStarted() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.path("wifi")
        rig.host.releaseApply()
        rig.path("cell")
        rig.host.releaseApply()
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.clock.pending, [1.5])
    }

    func testSameNetworkThroughoutStartDoesNotReconnect() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.path("wifi")
        rig.host.releaseApply()
        rig.host.releaseApply()
        rig.path("wifi")
        XCTAssertEqual(rig.clock.pending, [])
    }

    func testFirstAttemptRestartsInPlaceWithoutHubOrSettings() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, ["networkChanged", "reload", "stop", "start(7)"])
        XCTAssertEqual(rig.host.applyCount, 0)
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.host.reassertingHistory, [true, false])
        XCTAssertEqual(rig.events, [.reconnecting, .restored])
    }

    func testFailedRestartInPlaceFallsBackToTheFullSequenceWithoutReapplyingSettings() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, [
            "networkChanged", "reload", "stop", "start(7)", "stop", "prepare(srv)", "start(7)",
        ])
        XCTAssertEqual(rig.host.applyCount, 0)
        XCTAssertFalse(rig.host.reasserting)
    }

    func testMissingInterfaceSkipsTheRestartInPlace() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.host.fd = -1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, ["networkChanged", "reload", "stop", "stop", "prepare(srv)"])
        XCTAssertEqual(rig.clock.pending, [3])
        XCTAssertTrue(rig.host.reasserting)
    }

    func testLaterAttemptsReloadThenRunTheFullSequence() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 2
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.clock.pending, [3])
        rig.core.clearCalls()
        rig.clock.advance(by: 3)
        XCTAssertEqual(rig.core.calls, ["reload", "stop", "prepare(srv)", "start(7)"])
        XCTAssertFalse(rig.host.reasserting)
    }

    func testEveryAttemptReloadsButOnlyTheFirstInvalidatesTheNetwork() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1_000
        rig.path("cell")
        rig.clock.advance(by: 1.5 + 3 + 6 + 9 + 30)
        XCTAssertEqual(rig.core.calls.filter { $0 == "reload" }.count, 5)
        XCTAssertEqual(rig.core.calls.filter { $0 == "networkChanged" }.count, 1)
    }

    func testReconnectNeverGivesUp() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1_000
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        var delays: [TimeInterval] = []
        for _ in 0..<6 {
            delays += rig.clock.pending
            rig.clock.advance(by: rig.clock.pending.first ?? 0)
        }
        XCTAssertEqual(delays, [3, 6, 9, 30, 30, 30])
        XCTAssertTrue(rig.host.reasserting)
        XCTAssertTrue(rig.host.monitoring)
        XCTAssertEqual(rig.note.value, TunnelStartError.connectFailed.errorDescription)
        XCTAssertEqual(rig.events, [.reconnecting])
    }

    func testReconnectErrorIsSurfacedThenClearedOnSuccess() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 2
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.note.value, TunnelStartError.connectFailed.errorDescription)
        rig.clock.advance(by: 3)
        XCTAssertNil(rig.note.value)
    }

    func testWifiCellWifiRestartsTheDebounceOnlyOnARealChange() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.path("cell")
        rig.clock.advance(by: 1)
        rig.path("cell")
        XCTAssertEqual(rig.clock.pending, [0.5])
        rig.path("wifi")
        XCTAssertEqual(rig.clock.pending, [1.5])
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls.filter { $0 == "networkChanged" }.count, 1)
    }

    func testLostNetworkStopsGoAndWaitsThenReconnectsOnTheSameNetwork() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.clock.advance(by: 10)
        rig.path("", usable: false)
        XCTAssertEqual(rig.core.calls, ["stop"])
        XCTAssertTrue(rig.host.reasserting)
        XCTAssertEqual(rig.clock.pending, [])
        rig.clock.advance(by: 120)
        XCTAssertEqual(rig.core.calls, ["stop"])
        rig.path("wifi")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, ["stop", "networkChanged", "reload", "stop", "start(7)"])
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.events, [.reconnecting, .restored])
    }

    func testFailedAttemptOnALostNetworkWaitsInsteadOfRetrying() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.clock.advance(by: 10)
        rig.core.startFailures = 1_000
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.clock.pending, [3])
        rig.path("", usable: false)
        XCTAssertEqual(rig.clock.pending, [])
        XCTAssertTrue(rig.host.reasserting)
        rig.path("cell")
        XCTAssertEqual(rig.clock.pending, [1.5])
    }

    func testFlickerRightAfterOwnSettingsApplyIsAbsorbed() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.clock.advance(by: 1)
        rig.path("", usable: false)
        XCTAssertEqual(rig.clock.pending, [4])
        rig.path("wifi")
        rig.clock.advance(by: 4)
        XCTAssertEqual(rig.core.calls, [])
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.clock.pending, [])
    }

    func testOutageRightAfterOwnSettingsApplyStillWaits() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.clock.advance(by: 1)
        rig.path("", usable: false)
        rig.clock.advance(by: 4)
        XCTAssertEqual(rig.core.calls, ["stop"])
        XCTAssertTrue(rig.host.reasserting)
    }

    func testStopDuringBackoffCancelsEveryRetry() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1_000
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.clock.pending, [3])
        rig.lifecycle.stop()
        XCTAssertEqual(rig.clock.pending, [])
        XCTAssertEqual(rig.ticker.pending, [])
        XCTAssertFalse(rig.host.monitoring)
        XCTAssertNil(rig.note.value)
        let callsAtStop = rig.core.calls.count
        rig.path("wifi")
        rig.clock.advance(by: 120)
        XCTAssertEqual(rig.core.calls.count, callsAtStop)
        XCTAssertEqual(rig.events, [.reconnecting, .stopped])
    }

    func testRestartAfterStopResetsTheSession() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.lifecycle.stop()
        rig.core.clearCalls()
        rig.start()
        XCTAssertEqual(rig.outcomes, [nil, nil])
        XCTAssertEqual(rig.core.calls.first, "reload")
        rig.path("cell")
        XCTAssertEqual(rig.clock.pending, [])
    }
}
