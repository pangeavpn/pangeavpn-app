import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelLifecycleStartTests: XCTestCase {
    private let expired = CoreError(message: "hub: SUBSCRIPTION_EXPIRED")

    func testBlockingSettingsGoUpBeforeTheHubIsReached() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        XCTAssertEqual(rig.core.calls, [])
        XCTAssertEqual(rig.host.applyCount, 1)
        let blocking = rig.host.applied.first
        XCTAssertEqual(blocking?.ipv4Settings?.addresses, [TunnelSettingsBuilder.blockingConfig.address])
        XCTAssertEqual(blocking?.ipv4Settings?.includedRoutes?.count, 1)
        XCTAssertEqual(blocking?.ipv6Settings?.includedRoutes?.count, 1)
        XCTAssertEqual(blocking?.dnsSettings?.servers, TunnelSettingsBuilder.blockingConfig.dns)
        rig.host.releaseApply()
        XCTAssertEqual(rig.core.calls, ["reload", "restore", "config", "prepare(srv)"])
        XCTAssertEqual(rig.host.applyCount, 2)
    }

    func testFailedBlockingApplyFailsEvenASystemStart() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start(options: nil)
        rig.host.releaseApply(NSError(domain: "NEVPNErrorDomain", code: 1))
        XCTAssertEqual(rig.outcomes, [TunnelStart.otherErrorCode])
        XCTAssertEqual(rig.core.calls, [])
        XCTAssertFalse(rig.host.monitoring)
    }

    func testSystemStartCompletesDespiteAFailingPrepareThenReconnects() {
        let rig = LifecycleRig()
        rig.core.prepareErrors["last"] = LifecycleCoreFake.failure
        rig.start(options: nil)
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertTrue(rig.host.reasserting)
        XCTAssertTrue(rig.host.monitoring)
        XCTAssertEqual(rig.host.applyCount, 1)
        XCTAssertEqual(rig.clock.pending, [3])
        XCTAssertEqual(rig.events, [.reconnecting])
        XCTAssertEqual(rig.note.value, TunnelStartError.connectFailed.errorDescription)
        rig.core.prepareErrors = [:]
        rig.core.clearCalls()
        rig.clock.advance(by: 3)
        XCTAssertEqual(rig.core.calls, ["reload", "stop", "prepare(last)", "start(7)"])
        XCTAssertEqual(rig.host.applyCount, 2)
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertNil(rig.note.value)
        XCTAssertEqual(rig.events, [.reconnecting, .connected])
    }

    func testSystemStartWithoutTheHubRestoresAgainOnTheNextAttempt() {
        let rig = LifecycleRig()
        rig.core.restoreError = CoreError(message: "hub unreachable: every connection method failed")
        rig.start(options: nil)
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.clock.pending, [3])
        rig.core.restoreError = nil
        rig.core.clearCalls()
        rig.clock.advance(by: 3)
        XCTAssertEqual(rig.core.calls, ["reload", "stop", "restore", "config", "prepare(last)", "start(7)"])
        XCTAssertFalse(rig.host.reasserting)
    }

    func testTerminalErrorKeepsASystemStartUpAndBlockedWithALongBackoff() {
        let rig = LifecycleRig()
        rig.core.prepareErrors["last"] = expired
        rig.start(options: nil)
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.events, [.blocked(.subscriptionExpired)])
        XCTAssertEqual(rig.clock.pending, [300])
        XCTAssertTrue(rig.host.reasserting)
        XCTAssertEqual(rig.note.value, TunnelStartError.subscriptionExpired.errorDescription)
        rig.core.clearCalls()
        rig.clock.advance(by: 300)
        XCTAssertEqual(rig.core.calls, ["reload", "stop", "restore", "config", "prepare(last)"])
        XCTAssertEqual(rig.events, [.blocked(.subscriptionExpired)])
        XCTAssertEqual(rig.clock.pending, [300])
    }

    func testSignedOutSystemStartStaysBlocked() {
        let rig = LifecycleRig()
        rig.core.restoreError = CoreError(message: "mobile: no stored session")
        rig.start(options: nil)
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.events, [.blocked(.signedOut)])
        XCTAssertEqual(rig.clock.pending, [300])
    }

    func testSystemStartWithNoServerWaitsForOneToBeChosen() {
        let rig = LifecycleRig()
        rig.core.settings.lastServerId = ""
        rig.start(options: nil)
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.events, [.blocked(.noServer)])
        XCTAssertEqual(rig.clock.pending, [300])
        rig.core.settings.lastServerId = "last"
        rig.clock.advance(by: 300)
        XCTAssertTrue(rig.core.calls.contains("prepare(last)"))
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.events, [.blocked(.noServer), .connected])
    }

    func testTerminalErrorStaysBlockedWhileRunning() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1
        rig.core.prepareErrors["srv"] = expired
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.events, [.reconnecting, .blocked(.subscriptionExpired)])
        XCTAssertEqual(rig.clock.pending, [300])
        XCTAssertTrue(rig.host.reasserting)
        rig.core.prepareErrors = [:]
        rig.clock.advance(by: 300)
        XCTAssertEqual(rig.events, [.reconnecting, .blocked(.subscriptionExpired), .restored])
    }

    func testLockedKeychainRetriesEveryFiveSecondsOnASystemStart() {
        let rig = LifecycleRig()
        rig.core.locked = .deviceLocked
        rig.start(options: nil)
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.core.calls, ["reload"])
        XCTAssertEqual(rig.clock.pending, [5])
        XCTAssertEqual(rig.events, [])
        rig.clock.advance(by: 5)
        XCTAssertEqual(rig.clock.pending, [5])
        rig.core.locked = nil
        rig.clock.advance(by: 5)
        XCTAssertTrue(rig.core.calls.contains("start(7)"))
        XCTAssertEqual(rig.events, [.connected])
    }

    func testLockedKeychainFailsAnAppStartAtOnce() {
        let rig = LifecycleRig()
        rig.core.locked = .deviceLocked
        rig.start()
        XCTAssertEqual(rig.outcomes, [TunnelStartError.deviceLocked.rawValue])
        XCTAssertFalse(rig.core.calls.contains("restore"))
        XCTAssertFalse(rig.host.monitoring)
    }

    func testAppStartWalksTheCandidatesInOrder() {
        let rig = LifecycleRig()
        rig.core.prepareErrors = ["a": LifecycleCoreFake.failure, "b": LifecycleCoreFake.failure]
        rig.start(server: "a", candidates: ["a", "b", "c"])
        XCTAssertEqual(rig.core.calls, [
            "reload", "restore", "config", "prepare(a)", "prepare(b)", "prepare(c)", "start(7)",
        ])
        XCTAssertEqual(rig.outcomes, [nil])
    }

    func testAppStartFailsOnceEveryCandidateFailed() {
        let rig = LifecycleRig()
        rig.core.prepareErrors = ["a": LifecycleCoreFake.failure, "b": LifecycleCoreFake.failure]
        rig.start(server: "a", candidates: ["b"])
        XCTAssertEqual(rig.prepares(), ["prepare(a)", "prepare(b)"])
        XCTAssertEqual(rig.outcomes, [TunnelStartError.connectFailed.rawValue])
        XCTAssertFalse(rig.host.monitoring)
    }

    func testAccountErrorStopsTheCandidateWalk() {
        let rig = LifecycleRig()
        rig.core.prepareErrors["a"] = expired
        rig.start(server: "a", candidates: ["b"])
        XCTAssertEqual(rig.prepares(), ["prepare(a)"])
        XCTAssertEqual(rig.outcomes, [TunnelStartError.subscriptionExpired.rawValue])
    }

    func testUnknownServerMovesOnToTheNextCandidate() {
        let rig = LifecycleRig()
        rig.core.prepareErrors["a"] = CoreError(message: "mobile: unknown server \"a\"")
        rig.start(server: "a", candidates: ["b"])
        XCTAssertEqual(rig.prepares(), ["prepare(a)", "prepare(b)"])
        XCTAssertEqual(rig.outcomes, [nil])
    }

    func testEveryThirdFailedReconnectMovesToTheNextCandidate() {
        let rig = LifecycleRig()
        rig.start(server: "a", candidates: ["b"])
        _ = rig.settled(on: "wifi")
        rig.core.startFailures = 1
        rig.core.prepareErrors["a"] = LifecycleCoreFake.failure
        rig.path("cell")
        rig.clock.advance(by: 1.5 + 3 + 6)
        XCTAssertEqual(rig.prepares(), ["prepare(a)", "prepare(a)", "prepare(a)"])
        XCTAssertEqual(rig.clock.pending, [9])
        rig.clock.advance(by: 9)
        XCTAssertEqual(rig.prepares(), ["prepare(a)", "prepare(a)", "prepare(a)", "prepare(b)"])
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.events, [.reconnecting, .restored])
    }

    func testMultihopOffPreparesDirectly() {
        let rig = LifecycleRig()
        rig.core.entries = ["e1"]
        rig.start()
        XCTAssertEqual(rig.prepares(), ["prepare(srv)"])
        XCTAssertFalse(rig.core.calls.contains("entries(srv)"))
    }

    func testAutomaticMultihopTakesTheFirstEntry() {
        let rig = LifecycleRig()
        rig.useMultihop(entry: "")
        rig.core.entries = ["e1", "e2"]
        rig.start()
        XCTAssertEqual(rig.core.calls, [
            "reload", "restore", "config", "entries(srv)", "prepareVia(srv,e1)", "start(7)",
        ])
    }

    func testAutomaticMultihopWithoutEntriesFallsBackToDirect() {
        let rig = LifecycleRig()
        rig.useMultihop(entry: nil)
        rig.start()
        XCTAssertEqual(rig.prepares(), ["prepare(srv)"])
        XCTAssertTrue(rig.core.calls.contains("entries(srv)"))
        XCTAssertEqual(rig.outcomes, [nil])
    }

    func testChosenMultihopEntryIsUsedAsIs() {
        let rig = LifecycleRig()
        rig.useMultihop(entry: "e9")
        rig.start()
        XCTAssertEqual(rig.prepares(), ["prepareVia(srv,e9)"])
        XCTAssertFalse(rig.core.calls.contains("entries(srv)"))
    }

    func testReconnectKeepsTheMultihopRoute() {
        let rig = LifecycleRig()
        rig.useMultihop(entry: "")
        rig.core.entries = ["e1"]
        _ = rig.running(on: "wifi")
        rig.core.startFailures = 1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, [
            "networkChanged", "reload", "stop", "start(7)", "stop", "entries(srv)", "prepareVia(srv,e1)", "start(7)",
        ])
    }

    func testChangedAddressIsAppliedOnReconnect() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.tunnel = LifecycleCoreFake.tunnelConfig(dns: ["10.66.0.1"], address: "10.66.0.9")
        rig.core.startFailures = 1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.host.applyCount, 1)
        XCTAssertEqual(rig.host.lastSettings?.ipv4Settings?.addresses, ["10.66.0.9"])
    }

    func testChangedSplitRangesAreAppliedOnReconnect() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.useSplitTunnel(["203.0.113.0/24"])
        rig.core.startFailures = 1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.host.applyCount, 1)
        let excluded = (rig.host.lastSettings?.ipv4Settings?.excludedRoutes ?? []).map(\.destinationAddress)
        XCTAssertEqual(excluded, ["203.0.113.0"])
    }

    func testSplitRangesAreCarvedOutOfTheBlockingSettingsToo() {
        let rig = LifecycleRig()
        rig.useSplitTunnel(["203.0.113.0/24"])
        rig.host.holdApply = true
        rig.start()
        let excluded = (rig.host.applied.first?.ipv4Settings?.excludedRoutes ?? []).map(\.destinationAddress)
        XCTAssertEqual(excluded, ["203.0.113.0"])
    }

    func testUnansweredApplyIsRepeatedOnTheNextFullAttempt() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.host.releaseApply()
        rig.clock.advance(by: 5.1)
        XCTAssertEqual(rig.outcomes, [nil])
        rig.host.holdApply = false
        _ = rig.settled(on: "wifi")
        rig.core.startFailures = 1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.host.applyCount, 1)
    }
}

final class MultihopChoiceTests: XCTestCase {
    func testOffIsDirectWithoutAskingForEntries() {
        let choice = MultihopChoice.choose(preference: nil, exit: "x") {
            XCTFail("entries must not be read while multihop is off")
            return []
        }
        XCTAssertEqual(choice, .direct)
    }

    func testAutomaticTakesTheLightestEntry() {
        XCTAssertEqual(MultihopChoice.choose(preference: "", exit: "x") { ["e1", "e2"] }, .via("e1"))
        XCTAssertEqual(MultihopChoice.choose(preference: "", exit: "x") { [] }, .direct)
    }

    func testChosenEntryIsKeptUnlessItIsTheExit() {
        XCTAssertEqual(MultihopChoice.choose(preference: "e9", exit: "x") { ["e1"] }, .via("e9"))
        XCTAssertEqual(MultihopChoice.choose(preference: "x", exit: "x") { ["e1"] }, .via("e1"))
    }
}
