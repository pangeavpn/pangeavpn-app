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

    func testAccountProblemsAreTerminalAndEverythingElseRetries() {
        let terminal: [(Error, TunnelStartError)] = [
            (CoreError(message: "hub: SUBSCRIPTION_EXPIRED"), .subscriptionExpired),
            (CoreError(message: "hub: NO_SUBSCRIPTION"), .noSubscription),
            (CoreError(message: "mobile: not authenticated"), .signedOut),
            (CoreError(message: "mobile: unknown server \"x\""), .noServer),
            (TunnelStartError.signedOut, .signedOut),
            (TunnelStartError.noServer, .noServer),
        ]
        for (error, expected) in terminal {
            XCTAssertEqual(ReconnectPolicy.classify(error), .terminal(expected))
        }
        XCTAssertEqual(ReconnectPolicy.classify(TunnelStartError.deviceLocked), .locked)
        XCTAssertEqual(ReconnectPolicy.classify(LifecycleCoreFake.failure), .transient)
        XCTAssertEqual(ReconnectPolicy.classify(TunnelStartError.invalidDNS), .transient)
        XCTAssertEqual(ReconnectPolicy.classify(NSError(domain: "x", code: 1)), .transient)
    }

    func testRetryDelayDependsOnTheFailure() {
        let policy = ReconnectPolicy()
        XCTAssertEqual(policy.delay(after: .transient, attempt: 2), 6)
        XCTAssertEqual(policy.delay(after: .transient, attempt: 9), 30)
        XCTAssertEqual(policy.delay(after: .locked, attempt: 9), 5)
        XCTAssertEqual(policy.delay(after: .terminal(.subscriptionExpired), attempt: 1), 300)
    }

    func testEveryThirdFailureRotates() {
        let policy = ReconnectPolicy()
        XCTAssertEqual((0...7).filter { policy.rotates(afterFailedAttempt: $0) }, [3, 6])
    }
}

final class CandidateListTests: XCTestCase {
    func testAdvanceWrapsAround() {
        let list = CandidateList(["a", "b", "c"])
        XCTAssertEqual(list.current, "a")
        XCTAssertEqual(list.advanced().current, "b")
        XCTAssertEqual(list.advanced().advanced().advanced().current, "a")
    }

    func testEmptyAndSingleListsStayPut() {
        XCTAssertEqual(CandidateList([]).current, "")
        XCTAssertEqual(CandidateList([]).advanced(), CandidateList([]))
        XCTAssertEqual(CandidateList(["a"]).advanced(), CandidateList(["a"]))
    }

    func testIndexIsClamped() {
        XCTAssertEqual(CandidateList(["a", "b"], index: 9).current, "b")
        XCTAssertEqual(CandidateList(["a", "b"], index: -1).current, "a")
    }

    func testSwitchOrderIsNewThenOldThenTheRestWithoutRepeats() {
        let list = CandidateList.forSwitch(to: "b", from: "a", listed: ["b", "c", "a", "", "d"])
        XCTAssertEqual(list.ids, ["b", "a", "c", "d"])
        XCTAssertEqual(list.current, "b")
    }

    func testSwitchWithoutAPreviousServer() {
        XCTAssertEqual(CandidateList.forSwitch(to: "b", from: "", listed: []).ids, ["b"])
    }
}
