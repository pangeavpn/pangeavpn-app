import XCTest
@testable import PangeaVPN

private let signedIn = Session(email: "a@example.org", name: "A", servers: [])

private func device(_ id: String) -> Device {
    Device(id: id, friendlyName: "Phone \(id)", createdAt: "2026-01-01T00:00:00Z", status: "active")
}

@MainActor
final class SessionStoreTests: XCTestCase {
    private func makeStore(_ core: FakeCore, tunnel: FakeTunnel? = nil) -> SessionStore {
        SessionStore(core: core, tunnel: tunnel ?? FakeTunnel())
    }

    func testLoginNormalizesToken() async {
        let core = FakeCore()
        core.loginResult = .success(signedIn)
        let store = makeStore(core)
        await store.login("o1il-0000")
        let token = AccountNumber.normalize("o1il-0000")
        XCTAssertEqual(core.calls.first, "login(\(token))")
        XCTAssertEqual(store.session, signedIn)
        XCTAssertNil(store.loginError)
    }

    func testLoginLoadsSubscription() async {
        let core = FakeCore()
        let active = Subscription(status: "active", renews: true, expiresAt: nil)
        core.loginResult = .success(signedIn)
        core.subscriptionResult = .success(active)
        let store = makeStore(core)
        await store.login("ABCD-EFGH")
        XCTAssertEqual(store.subscription, active)
        XCTAssertEqual(core.calls.last, "subscription")
    }

    func testBlankLoginShowsErrorWithoutCoreCall() async {
        let core = FakeCore()
        let store = makeStore(core)
        await store.login("  -  ")
        XCTAssertEqual(store.loginError, "Enter your account number")
        XCTAssertTrue(core.calls.isEmpty)
    }

    func testLoginDeviceLimitShowsScreen() async {
        let core = FakeCore()
        core.loginResult = .failure(CoreError(message: "mobile: register device: device limit reached"))
        let store = makeStore(core)
        await store.login("ABCD-EFGH")
        XCTAssertTrue(store.showDeviceLimit)
        XCTAssertNil(store.loginError)
        XCTAssertNil(store.session)
    }

    func testLoginOtherErrorShowsMessage() async {
        let core = FakeCore()
        core.loginResult = .failure(CoreError(message: "mobile: invalid account number"))
        let store = makeStore(core)
        await store.login("ABCD-EFGH")
        XCTAssertEqual(store.loginError, "mobile: invalid account number")
        XCTAssertFalse(store.showDeviceLimit)
        XCTAssertNil(store.session)
    }

    func testSubscriptionFailureDoesNotFailLogin() async {
        let core = FakeCore()
        core.loginResult = .success(signedIn)
        core.subscriptionResult = .failure(CoreError(message: "mobile: hub unreachable"))
        let store = makeStore(core)
        await store.login("ABCD-EFGH")
        XCTAssertEqual(store.session, signedIn)
        XCTAssertNil(store.subscription)
        XCTAssertNil(store.loginError)
    }

    func testRetryLoginReusesTheNormalizedToken() async {
        let core = FakeCore()
        core.loginResult = .failure(CoreError(message: "device_limit"))
        let store = makeStore(core)
        await store.login("abcd-efgh")
        XCTAssertTrue(store.showDeviceLimit)
        core.loginResult = .success(signedIn)
        await store.retryLogin()
        let token = AccountNumber.normalize("abcd-efgh")
        XCTAssertEqual(core.calls.filter { $0.hasPrefix("login(") }, ["login(\(token))", "login(\(token))"])
        XCTAssertFalse(store.showDeviceLimit)
        XCTAssertEqual(store.session, signedIn)
    }

    func testRetryFailingForAnotherReasonStaysOnTheDeviceLimitScreen() async {
        let core = FakeCore()
        core.loginResult = .failure(CoreError(message: "device_limit"))
        let store = makeStore(core)
        await store.login("ABCD-EFGH")
        core.loginResult = .failure(CoreError(message: "mobile: hub unreachable"))
        await store.retryLogin()
        XCTAssertTrue(store.showDeviceLimit)
        XCTAssertEqual(store.loginError, "mobile: hub unreachable")
        XCTAssertNil(store.session)
    }

    func testRemoveDeviceRefreshesList() async {
        let core = FakeCore()
        core.devicesResult = .success([device("d1"), device("d2")])
        let store = makeStore(core)
        await store.loadDevices()
        XCTAssertEqual(store.devices, [device("d1"), device("d2")])
        core.devicesResult = .success([device("d2")])
        await store.removeDevice("d1")
        XCTAssertEqual(store.devices, [device("d2")])
        XCTAssertEqual(Array(core.calls.suffix(2)), ["removeDevice(d1)", "listDevices"])
    }

    func testRemoveDeviceFailureKeepsListAndShowsError() async {
        let core = FakeCore()
        core.devicesResult = .success([device("d1")])
        let store = makeStore(core)
        await store.loadDevices()
        core.removeDeviceResult = .failure(CoreError(message: "mobile: remove failed"))
        await store.removeDevice("d1")
        XCTAssertEqual(store.devices, [device("d1")])
        XCTAssertEqual(store.deviceError, "mobile: remove failed")
    }

    func testUnsavedSignInShowsTheErrorAndNoSession() async {
        let core = FakeCore()
        core.loginResult = .failure(.signInNotSaved)
        let store = makeStore(core)
        await store.login("ABCD-EFGH")
        XCTAssertNil(store.session)
        XCTAssertEqual(store.loginError, CoreError.signInNotSaved.message)
        XCTAssertFalse(store.showDeviceLimit)
        XCTAssertFalse(core.calls.contains("subscription"))
    }

    func testOverlappingLoginsReachTheCoreOnce() async {
        let core = FakeCore()
        core.loginResult = .success(signedIn)
        let store = makeStore(core)
        async let first: Void = store.login("ABCD-EFGH")
        async let second: Void = store.login("ABCD-EFGH")
        _ = await (first, second)
        XCTAssertEqual(core.calls.filter { $0.hasPrefix("login(") }.count, 1)
        XCTAssertEqual(store.session, signedIn)
        XCTAssertFalse(store.loggingIn)
    }

    func testRestoreSetsSessionAndLoadsSubscription() async {
        let core = FakeCore()
        core.restoreResult = .success(signedIn)
        let tunnel = FakeTunnel()
        let store = makeStore(core, tunnel: tunnel)
        await store.restore()
        XCTAssertEqual(store.session, signedIn)
        XCTAssertFalse(store.restoring)
        XCTAssertEqual(Array(core.calls.dropFirst()), ["restoreSession", "subscription"])
        XCTAssertEqual(tunnel.removals, 0)
    }

    func testRestoreFailuresLeaveNoSessionAndNoError() async {
        for message in ["mobile: no stored session", "mobile: no device identity", "mobile: hub unreachable"] {
            let core = FakeCore()
            core.restoreResult = .failure(CoreError(message: message))
            let store = makeStore(core)
            await store.restore()
            XCTAssertNil(store.session, message)
            XCTAssertFalse(store.restoring, message)
            XCTAssertNil(store.loginError, message)
            XCTAssertEqual(Array(core.calls.dropFirst()), ["restoreSession"], message)
        }
    }

    func testRestoreWithNoStoredSessionRemovesTheProfile() async {
        for message in ["mobile: no stored session", "mobile: no device identity"] {
            let core = FakeCore()
            core.restoreResult = .failure(CoreError(message: message))
            let tunnel = FakeTunnel(onRemove: { core.note("removeConfiguration") })
            let store = makeStore(core, tunnel: tunnel)
            await store.restore()
            XCTAssertEqual(tunnel.removals, 1, message)
            XCTAssertEqual(Array(core.calls.suffix(2)), ["restoreSession", "removeConfiguration"], message)
            XCTAssertNil(store.session, message)
        }
    }

    // An unreachable hub says nothing about the session, so the profile and its kill switch stay.
    func testRestoreThatCouldNotCheckKeepsTheProfile() async {
        let core = FakeCore()
        core.restoreResult = .failure(CoreError(message: "mobile: hub unreachable: retrying in 30s"))
        let tunnel = FakeTunnel()
        let store = makeStore(core, tunnel: tunnel)
        await store.restore()
        XCTAssertEqual(tunnel.removals, 0)
        XCTAssertNil(store.session)
    }

    func testRestoreSendsClientInfoOnceAndFirst() async {
        let core = FakeCore()
        core.restoreResult = .success(signedIn)
        let store = makeStore(core)
        await store.restore()
        await store.restore()
        XCTAssertEqual(core.calls.filter { $0.hasPrefix("setClientInfo(") }.count, 1)
        XCTAssertEqual(core.calls.first?.hasPrefix("setClientInfo("), true)
    }

    func testClientInfoStrings() {
        XCTAssertEqual(ClientInfo.appVersion(short: "0.8.2", build: "41"), "0.8.2 (41)")
        XCTAssertEqual(ClientInfo.appVersion(short: "0.8.2", build: "0.8.2"), "0.8.2")
        XCTAssertEqual(ClientInfo.appVersion(short: "0.8.2", build: nil), "0.8.2")
        XCTAssertEqual(ClientInfo.appVersion(short: nil, build: ""), "unknown")
        let version = OperatingSystemVersion(majorVersion: 17, minorVersion: 4, patchVersion: 1)
        XCTAssertEqual(ClientInfo.osVersion(version), "17.4.1")
    }

    func testEntitledIsFalseOnlyOnAnExplicitNo() async {
        let cases: [(Subscription?, Bool)] = [
            (nil, true),
            (Subscription(status: "active", renews: true, expiresAt: nil), true),
            (Subscription(status: "active", renews: true, expiresAt: nil, entitled: true), true),
            (Subscription(status: "expired", renews: false, expiresAt: nil, entitled: false), false),
        ]
        for (subscription, expected) in cases {
            let core = FakeCore()
            core.subscriptionResult = .success(subscription)
            let store = makeStore(core)
            await store.refreshSubscription()
            XCTAssertEqual(store.entitled, expected, String(describing: subscription))
            XCTAssertEqual(core.calls, ["subscription"])
        }
    }

    func testEntitledBeforeAnyAnswer() {
        XCTAssertTrue(makeStore(FakeCore()).entitled)
    }

    func testRefreshSaysWhetherAnAnswerCameBack() async {
        let core = FakeCore()
        let active = Subscription(status: "active", renews: true, expiresAt: nil)
        core.subscriptionResult = .success(active)
        let store = makeStore(core)
        let answered = await store.refreshSubscription()
        XCTAssertTrue(answered)
        core.subscriptionResult = .failure(CoreError(message: "mobile: hub unreachable"))
        let unanswered = await store.refreshSubscription()
        XCTAssertFalse(unanswered)
        XCTAssertEqual(store.subscription, active)
    }

    func testACachedAnswerStillCountsAsAnAnswer() async {
        let core = FakeCore()
        core.subscriptionResult = .success(Subscription(
            status: "expired", renews: false, expiresAt: nil, entitled: false, cached: true))
        let store = makeStore(core)
        let answered = await store.refreshSubscription()
        XCTAssertTrue(answered)
        XCTAssertEqual(store.subscription?.cached, true)
    }

    func testAnAnswerLandingAfterSignOutIsDropped() async {
        let core = FakeCore()
        core.loginResult = .success(signedIn)
        let store = makeStore(core)
        await store.login("ABCD-EFGH")
        core.subscriptionResult = .success(Subscription(
            status: "expired", renews: false, expiresAt: nil, entitled: false))
        core.subscriptionDelay = 1
        let late = Task { await store.refreshSubscription() }
        let started = await eventually { core.calls.filter { $0 == "subscription" }.count == 2 }
        XCTAssertTrue(started)
        await store.signOut()
        let answered = await late.value
        XCTAssertFalse(answered)
        XCTAssertNil(store.subscription)
        XCTAssertTrue(store.entitled)
    }
}
