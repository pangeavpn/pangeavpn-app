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

    func testSignOutDisconnectsAndRemovesTheProfileBeforeLogout() async {
        let core = FakeCore()
        let tunnel = FakeTunnel(
            onDisconnect: { core.note("disconnect") },
            onRemove: { core.note("removeConfiguration") })
        core.loginResult = .success(signedIn)
        core.devicesResult = .success([device("d1")])
        let store = makeStore(core, tunnel: tunnel)
        await store.login("ABCD-EFGH")
        await store.loadDevices()
        await store.signOut()
        XCTAssertEqual(Array(core.calls.suffix(3)), ["disconnect", "removeConfiguration", "logout"])
        XCTAssertEqual(tunnel.disconnects, 1)
        XCTAssertEqual(tunnel.removals, 1)
        XCTAssertNil(store.session)
        XCTAssertNil(store.subscription)
        XCTAssertTrue(store.devices.isEmpty)
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
        let store = makeStore(core)
        await store.restore()
        XCTAssertEqual(store.session, signedIn)
        XCTAssertFalse(store.restoring)
        XCTAssertEqual(core.calls, ["restoreSession", "subscription"])
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
            XCTAssertEqual(core.calls, ["restoreSession"], message)
        }
    }
}
