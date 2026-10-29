import NetworkExtension
import XCTest
@testable import PangeaVPN

final class ReconnectPolicyTests: XCTestCase {
    func testFirstPathIsBaseline() {
        var policy = ReconnectPolicy()
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .ignore)
    }

    func testSameKeyIgnored() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .ignore)
    }

    func testNewKeyReconnectsAfterDebounce() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        XCTAssertEqual(policy.pathChanged(key: "b", usable: true, now: 0), .reconnect(after: 1.5))
        XCTAssertEqual(policy.pathChanged(key: "b", usable: true, now: 0), .ignore)
    }

    func testUnusablePathWaits() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        XCTAssertEqual(policy.pathChanged(key: "", usable: false, now: 0), .waitForNetwork)
        XCTAssertEqual(policy.pathChanged(key: "b", usable: true, now: 0), .reconnect(after: 1.5))
    }

    func testSameKeyAfterWaitingReconnects() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        _ = policy.pathChanged(key: "a", usable: false, now: 0)
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .reconnect(after: 1.5))
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .ignore)
    }

    func testUnusableFirstThenUsableReconnects() {
        var policy = ReconnectPolicy()
        XCTAssertEqual(policy.pathChanged(key: "", usable: false, now: 0), .waitForNetwork)
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .reconnect(after: 1.5))
    }

    func testRepeatedUnusableThenOneReconnect() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        for _ in 0..<3 { XCTAssertEqual(policy.pathChanged(key: "", usable: false, now: 0), .waitForNetwork) }
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .reconnect(after: 1.5))
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .ignore)
    }

    func testBackoffStepsThenHoldsAtThirtySecondsWithoutGivingUp() {
        let policy = ReconnectPolicy()
        XCTAssertEqual([1, 2, 3, 4, 5, 50].map { policy.backoff(attempt: $0) }, [3, 6, 9, 30, 30, 30])
    }

    func testEmptyKeyIsIgnored() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        XCTAssertEqual(policy.pathChanged(key: "", usable: true, now: 0), .ignore)
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .ignore)
    }

    func testEmptyKeyKeepsWaiting() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        _ = policy.pathChanged(key: "", usable: false, now: 0)
        XCTAssertEqual(policy.pathChanged(key: "", usable: true, now: 0), .ignore)
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .reconnect(after: 1.5))
    }

    func testSeededKeyIsTheBaseline() {
        var policy = ReconnectPolicy()
        policy.seed(key: "a")
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .ignore)
        XCTAssertEqual(policy.pathChanged(key: "b", usable: true, now: 0), .reconnect(after: 1.5))
    }

    func testUnusableSoonAfterOwnApplyIsRecheckedFirst() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        policy.settingsApplied(at: 10)
        XCTAssertEqual(policy.pathChanged(key: "", usable: false, now: 12), .recheck(after: 3))
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 13), .ignore)
        XCTAssertEqual(policy.pathChanged(key: "", usable: false, now: 15), .waitForNetwork)
    }

    func testMarkWaitingMakesTheNextUsablePathReconnect() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true, now: 0)
        policy.markWaiting()
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true, now: 0), .reconnect(after: 1.5))
    }
}

final class TunnelLifecycleTests: XCTestCase {
    func testStartReloadsThenRunsTheFullSequence() {
        let rig = LifecycleRig()
        rig.start()
        XCTAssertEqual(rig.core.calls, ["reload", "restore", "config", "prepare(srv)", "start(7)"])
        XCTAssertEqual(rig.host.applyCount, 1)
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertTrue(rig.host.monitoring)
        XCTAssertFalse(rig.host.reasserting)
    }

    func testStartWithoutNetworkFailsOnceWithMappedErrorAndStopsMonitoring() {
        let rig = LifecycleRig()
        rig.core.restoreError = CoreError(message: "hub unreachable: every connection method failed")
        rig.start()
        rig.clock.advance(by: 60)
        XCTAssertEqual(rig.outcomes, [TunnelStartError.connectFailed.rawValue])
        XCTAssertEqual(rig.messages, [TunnelStartError.connectFailed.errorDescription])
        XCTAssertFalse(rig.host.monitoring)
        XCTAssertEqual(rig.host.applyCount, 0)
    }

    func testSignedOutStartMapsToSignedOut() {
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

    func testConfigWithoutUsableDNSFailsStart() {
        let rig = LifecycleRig()
        rig.core.tunnel = LifecycleCoreFake.tunnelConfig(dns: ["127.0.0.1"])
        rig.start()
        XCTAssertEqual(rig.outcomes, [TunnelStartError.invalidDNS.rawValue])
        XCTAssertEqual(rig.host.applyCount, 0)
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
        rig.lifecycle.stop()
        XCTAssertEqual(rig.outcomes, [TunnelStartError.stopped.rawValue])
        rig.host.releaseApply()
        rig.clock.advance(by: 10)
        XCTAssertEqual(rig.outcomes.count, 1)
        XCTAssertFalse(rig.core.calls.contains("start(7)"))
        XCTAssertFalse(rig.host.monitoring)
    }

    func testSettingsApplyThatNeverCallsBackProceedsToTheFdProbe() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
        rig.clock.advance(by: 4.9)
        XCTAssertTrue(rig.outcomes.isEmpty)
        XCTAssertFalse(rig.core.calls.contains("start(7)"))
        rig.clock.advance(by: 0.2)
        XCTAssertEqual(rig.outcomes, [nil])
        rig.host.releaseApply(NSError(domain: "NEVPNErrorDomain", code: 1))
        XCTAssertEqual(rig.outcomes, [nil])
        XCTAssertEqual(rig.core.calls.filter { $0 == "start(7)" }.count, 1)
    }

    func testSettingsTimeoutWithoutAnInterfaceStillFailsAsNoTun() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.host.fd = -1
        rig.start()
        rig.clock.advance(by: 5.1)
        XCTAssertEqual(rig.outcomes, [TunnelStartError.noTunnelInterface.rawValue])
        rig.host.releaseApply()
        XCTAssertEqual(rig.outcomes.count, 1)
    }

    func testSettingsErrorBeforeTheTimeoutFailsTheStep() {
        let rig = LifecycleRig()
        rig.host.holdApply = true
        rig.start()
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
        rig.path("wifi")
        XCTAssertEqual(rig.clock.pending, [])
    }

    func testFirstAttemptRestartsInPlaceWithoutHubOrSettings() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, ["networkChanged", "reload", "stop", "start(7)"])
        XCTAssertEqual(rig.host.applyCount, 1)
        XCTAssertFalse(rig.host.reasserting)
        XCTAssertEqual(rig.host.reassertingHistory, [true, false])
    }

    func testFailedRestartInPlaceFallsBackToTheFullSequence() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.startFailures = 1
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        XCTAssertEqual(rig.core.calls, [
            "networkChanged", "reload", "stop", "start(7)", "stop", "prepare(srv)", "start(7)",
        ])
        XCTAssertEqual(rig.host.applyCount, 2)
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
    }

    func testFailedAttemptOnALostNetworkWaitsInsteadOfRetrying() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.clock.advance(by: 10)
        rig.core.startFailures = 1_000
        rig.path("cell")
        rig.clock.advance(by: 1.5)
        rig.path("", usable: false)
        XCTAssertEqual(rig.clock.pending, [3, 5])
        rig.clock.advance(by: 3)
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
        XCTAssertFalse(rig.host.monitoring)
        XCTAssertNil(rig.note.value)
        let callsAtStop = rig.core.calls.count
        rig.path("wifi")
        rig.clock.advance(by: 120)
        XCTAssertEqual(rig.core.calls.count, callsAtStop)
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

final class LifecycleRig {
    let core: LifecycleCoreFake
    let host: LifecycleHostFake
    let clock: LifecycleClock
    let note: ReconnectNote
    let lifecycle: TunnelLifecycle
    private(set) var outcomes: [Int?] = []
    private(set) var messages: [String?] = []

    init() {
        let core = LifecycleCoreFake()
        let host = LifecycleHostFake()
        let clock = LifecycleClock()
        let note = ReconnectNote()
        core.settings.lastServerId = "last"
        self.core = core
        self.host = host
        self.clock = clock
        self.note = note
        self.lifecycle = TunnelLifecycle(core: core, host: host, scheduler: clock, note: note)
    }

    func start(options: [String: NSObject]? = [TunnelStart.serverIdOption: "srv" as NSString],
               excludeLocal: Bool? = nil) {
        lifecycle.start(options: options, excludeLocalNetworks: excludeLocal) { [weak self] error in
            let ns = error.map { $0 as NSError }
            self?.outcomes.append(ns?.code)
            self?.messages.append(ns?.localizedDescription)
        }
    }

    func path(_ key: String, usable: Bool = true) {
        lifecycle.pathChanged(PathUpdate(usable: usable, key: key))
    }

    func running(on key: String) -> LifecycleRig {
        start()
        path(key)
        core.clearCalls()
        host.clearHistory()
        return self
    }
}

final class LifecycleCoreFake: LifecycleCore {
    static let failure = CoreError(message: "no transport could establish a tunnel: cloak: dial 203.0.113.9")

    static func tunnelConfig(dns: [String]) -> TunnelConfig {
        TunnelConfig(address: "10.66.0.2", prefixLength: 32, dns: dns, mtu: 1380, serverId: "srv", serverName: "n")
    }

    var settings = AppSettings()
    var tunnel = LifecycleCoreFake.tunnelConfig(dns: ["10.66.0.1"])
    var restoreError: Error?
    var startFailures = 0
    var reloadFailures = 0
    private(set) var calls: [String] = []

    func reload() throws {
        calls.append("reload")
        guard reloadFailures > 0 else { return }
        reloadFailures -= 1
        throw CoreError(message: "decode stored config: unexpected end of JSON input")
    }

    func networkChanged() { calls.append("networkChanged") }

    func restore() throws {
        calls.append("restore")
        if let restoreError { throw restoreError }
    }

    func config() throws -> AppSettings {
        calls.append("config")
        return settings
    }

    func prepare(serverId: String) throws -> TunnelConfig {
        calls.append("prepare(\(serverId))")
        return tunnel
    }

    func start(tunFd: Int) throws {
        calls.append("start(\(tunFd))")
        guard startFailures > 0 else { return }
        startFailures -= 1
        throw Self.failure
    }

    func stop() { calls.append("stop") }

    func clearCalls() { calls = [] }
}

final class LifecycleHostFake: LifecycleHost {
    var fd = 7
    var holdApply = false
    private(set) var reasserting = false
    private(set) var reassertingHistory: [Bool] = []
    private(set) var monitoring = false
    private(set) var applyCount = 0
    private(set) var lastSettings: NEPacketTunnelNetworkSettings?
    private var held: [(Error?) -> Void] = []

    func applyTunnelSettings(_ settings: NEPacketTunnelNetworkSettings, completion: @escaping (Error?) -> Void) {
        applyCount += 1
        lastSettings = settings
        if holdApply { held.append(completion) } else { completion(nil) }
    }

    func releaseApply(_ error: Error? = nil) {
        let pending = held
        held = []
        pending.forEach { $0(error) }
    }

    func findTunFD() -> Int { fd }

    func markReasserting(_ value: Bool) {
        reasserting = value
        if reassertingHistory.last != value { reassertingHistory.append(value) }
    }

    func watchPath(_ enabled: Bool) { monitoring = enabled }

    func clearHistory() { reassertingHistory = [] }
}

final class LifecycleClock: LifecycleScheduler {
    final class Job: LifecycleTimer {
        let due: TimeInterval
        let work: () -> Void
        private(set) var cancelled = false

        init(due: TimeInterval, work: @escaping () -> Void) {
            self.due = due
            self.work = work
        }

        func cancel() { cancelled = true }
    }

    private(set) var now: TimeInterval = 0
    private var jobs: [Job] = []

    var pending: [TimeInterval] { jobs.filter { !$0.cancelled }.map { $0.due - now } }

    func after(_ delay: TimeInterval, _ work: @escaping () -> Void) -> LifecycleTimer {
        let job = Job(due: now + delay, work: work)
        jobs.append(job)
        return job
    }

    func advance(by delta: TimeInterval) {
        let end = now + delta
        while let next = jobs.filter({ !$0.cancelled && $0.due <= end }).min(by: { $0.due < $1.due }) {
            jobs.removeAll { $0 === next || $0.cancelled }
            now = next.due
            next.work()
        }
        jobs.removeAll { $0.cancelled }
        now = end
    }
}
