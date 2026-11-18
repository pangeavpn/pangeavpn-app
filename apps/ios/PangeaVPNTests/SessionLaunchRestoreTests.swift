import XCTest
@testable import PangeaVPN

private let signedIn = Session(email: "a@example.org", name: "A", servers: [])
private let storedAccount = "ABCDEFGHJKMNPQRSTVWXYZ01"

// A hub search on a dead network can outlast any wait, so launch opens on the stored sign-in instead.
@MainActor
final class SessionLaunchRestoreTests: XCTestCase {
    private func slowCore(_ result: Result<Session, CoreError>, account: String = storedAccount) -> FakeCore {
        let core = FakeCore()
        core.restoreResult = result
        core.restoreDelay = 0.4
        core.accountNumberValue = account
        return core
    }

    private func makeStore(_ core: FakeCore, tunnel: FakeTunnel? = nil) -> SessionStore {
        SessionStore(core: core, tunnel: tunnel ?? FakeTunnel(), timeouts: SessionTimeouts(launchRestore: 0.05))
    }

    func testSlowRestoreOpensOnTheStoredSignInThenAdoptsTheAnswer() async {
        let core = slowCore(.success(signedIn))
        let store = makeStore(core)
        let started = Date()
        await store.restore()
        XCTAssertLessThan(Date().timeIntervalSince(started), 0.3)
        XCTAssertNotNil(store.session)
        XCTAssertFalse(store.restoring)
        XCTAssertFalse(store.restoreFailed)
        let adopted = await eventually { store.session == signedIn && core.calls.contains("subscription") }
        XCTAssertTrue(adopted)
    }

    func testSlowRestoreWithNoStoredAccountWaitsForTheAnswer() async {
        let core = slowCore(.failure(CoreError(message: "mobile: hub unreachable")), account: "")
        let store = makeStore(core)
        await store.restore()
        XCTAssertNil(store.session)
        XCTAssertTrue(store.restoreFailed)
        XCTAssertFalse(store.restoring)
    }

    func testLateSignedOutAnswerEndsTheSessionAndRemovesTheProfile() async {
        let core = slowCore(.failure(CoreError(message: "mobile: no stored session")))
        let tunnel = FakeTunnel()
        let store = makeStore(core, tunnel: tunnel)
        await store.restore()
        XCTAssertNotNil(store.session)
        let ended = await eventually { store.session == nil && tunnel.removals == 1 }
        XCTAssertTrue(ended)
        XCTAssertFalse(store.restoreFailed)
    }

    // The core drops its account when the hub ends the session, so the app follows it out.
    func testLateFailureThatSignedTheCoreOutEndsTheSession() async {
        let core = slowCore(.failure(CoreError(message: "SESSION_REVOKED")))
        let tunnel = FakeTunnel()
        let store = makeStore(core, tunnel: tunnel)
        await store.restore()
        XCTAssertNotNil(store.session)
        core.accountNumberValue = ""
        let ended = await eventually { store.session == nil && tunnel.removals == 1 }
        XCTAssertTrue(ended)
    }

    // Still unreachable says nothing about the session, so the open app and its profile stay.
    func testLateUnreachableAnswerKeepsTheAppOpen() async {
        let core = slowCore(.failure(CoreError(message: "mobile: hub unreachable")))
        let tunnel = FakeTunnel()
        let store = makeStore(core, tunnel: tunnel)
        await store.restore()
        let settled = await eventually { core.calls.contains("subscription") }
        XCTAssertTrue(settled)
        XCTAssertNotNil(store.session)
        XCTAssertFalse(store.restoreFailed)
        XCTAssertEqual(tunnel.removals, 0)
    }

    func testLateAnswerAfterSignOutIsDropped() async {
        let core = slowCore(.success(signedIn))
        let store = SessionStore(
            core: core, tunnel: FakeTunnel(),
            timeouts: SessionTimeouts(networkSettle: 0, launchRestore: 0.05))
        await store.restore()
        await store.signOut()
        XCTAssertNil(store.session)
        try? await Task.sleep(for: .milliseconds(600))
        XCTAssertNil(store.session)
        XCTAssertFalse(core.calls.contains("subscription"))
    }

    // The answer lands while the core is still logging out, before the session is cleared.
    func testLateAnswerDuringSignOutIsDropped() async {
        let core = slowCore(.success(signedIn))
        core.logoutDelay = 0.8
        let store = SessionStore(
            core: core, tunnel: FakeTunnel(),
            timeouts: SessionTimeouts(networkSettle: 0, launchRestore: 0.05))
        await store.restore()
        await store.signOut()
        XCTAssertNil(store.session)
        XCTAssertFalse(core.calls.contains("subscription"))
    }

    // A stored list needs no hub, so launch never waits on one.
    func testStoredServersOpenTheAppAtOnceAndTheAnswerReplacesThem() async {
        let core = slowCore(.success(Session(email: "", name: "", servers: [fresh])))
        core.restoreDelay = 3
        core.cachedServersValue = [stale]
        let store = SessionStore(core: core, tunnel: FakeTunnel(), timeouts: SessionTimeouts(launchRestore: 2))
        let started = Date()
        await store.restore()
        XCTAssertLessThan(Date().timeIntervalSince(started), 1)
        XCTAssertEqual(store.session?.servers, [stale])
        XCTAssertEqual(store.session?.cached, true)
        XCTAssertTrue(store.settling)
        XCTAssertFalse(store.restoring)
        let adopted = await eventually(within: .seconds(5)) { store.session?.servers == [fresh] && !store.settling }
        XCTAssertTrue(adopted)
    }

    func testStoredServersStillLeaveWhenTheLateAnswerIsSignedOut() async {
        let core = slowCore(.failure(CoreError(message: "mobile: no stored session")))
        core.cachedServersValue = [stale]
        let tunnel = FakeTunnel()
        let store = makeStore(core, tunnel: tunnel)
        await store.restore()
        XCTAssertNotNil(store.session)
        let ended = await eventually { store.session == nil && tunnel.removals == 1 && !store.settling }
        XCTAssertTrue(ended)
    }
}

private let stale = Server(id: "old-1", name: "Old", region: "eu", country: "DE", load: 10)
private let fresh = Server(id: "new-1", name: "New", region: "eu", country: "DE", load: 10)

