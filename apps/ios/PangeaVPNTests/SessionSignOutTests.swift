import XCTest
@testable import PangeaVPN

private let signedIn = Session(email: "a@example.org", name: "A", servers: [])

private let active = Subscription(status: "active", renews: true, expiresAt: nil)

private func device(_ id: String) -> Device {
    Device(id: id, friendlyName: "Phone \(id)", createdAt: "2026-01-01T00:00:00Z", status: "active")
}

@MainActor
final class SessionSignOutTests: XCTestCase {
    private func trackedTunnel(_ core: FakeCore) -> FakeTunnel {
        FakeTunnel(onDisconnect: { core.note("disconnect") }, onRemove: { core.note("removeConfiguration") })
    }

    // The live wiper is only a default here; tests that wipe always pass a fixture's.
    private func signedInStore(
        _ core: FakeCore, tunnel: FakeTunnel, wiper: PanicWiper = .live()
    ) async -> SessionStore {
        core.loginResult = .success(signedIn)
        core.subscriptionResult = .success(active)
        core.devicesResult = .success([device("d1")])
        let store = SessionStore(
            core: core, tunnel: tunnel, authenticator: FakeAuthenticator(.denied), wiper: wiper)
        await store.login("ABCD-EFGH")
        await store.loadDevices()
        return store
    }

    private func assertCleared(_ store: SessionStore, file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertNil(store.session, file: file, line: line)
        XCTAssertNil(store.subscription, file: file, line: line)
        XCTAssertTrue(store.devices.isEmpty, file: file, line: line)
        XCTAssertFalse(store.showDeviceLimit, file: file, line: line)
        XCTAssertNil(store.deviceError, file: file, line: line)
    }

    func testSignOutDeregistersAfterTheProfileIsGoneAndBeforeLogout() async {
        let core = FakeCore()
        let tunnel = trackedTunnel(core)
        let store = await signedInStore(core, tunnel: tunnel)
        await store.signOut()
        XCTAssertEqual(Array(core.calls.suffix(4)), ["disconnect", "removeConfiguration", "deregister", "logout"])
        XCTAssertEqual(tunnel.disconnects, 1)
        XCTAssertEqual(tunnel.removals, 1)
        XCTAssertFalse(store.signingOut)
        assertCleared(store)
    }

    func testSignOutStillLogsOutWhenDeregisterFails() async {
        let core = FakeCore()
        let store = await signedInStore(core, tunnel: trackedTunnel(core))
        core.deregisterResult = .failure(CoreError(message: "hub unreachable: retrying in 30s"))
        await store.signOut()
        XCTAssertEqual(Array(core.calls.suffix(2)), ["deregister", "logout"])
        XCTAssertNil(store.loginError)
        assertCleared(store)
    }

    func testSignOutGivesUpOnASlowDeregister() async {
        let fake = FakeCore()
        let core = GatedCore(fake)
        fake.loginResult = .success(signedIn)
        let store = SessionStore(
            core: core, tunnel: trackedTunnel(fake), timeouts: SessionTimeouts(deregister: 0.1))
        await store.login("ABCD-EFGH")
        let started = Date()
        await store.signOut()
        let elapsed = Date().timeIntervalSince(started)
        let calls = fake.calls
        core.release()
        XCTAssertLessThan(elapsed, 4)
        XCTAssertEqual(Array(calls.suffix(3)), ["disconnect", "removeConfiguration", "logout"])
        XCTAssertFalse(calls.contains("deregister"))
        XCTAssertNil(store.session)
        XCTAssertFalse(store.signingOut)
    }

    func testOverlappingSignOutsLogOutOnce() async {
        let core = FakeCore()
        let store = await signedInStore(core, tunnel: trackedTunnel(core))
        core.logoutDelay = 0.3
        async let first: Void = store.signOut()
        async let second: Void = store.signOut()
        _ = await (first, second)
        XCTAssertEqual(core.calls.filter { $0 == "logout" }.count, 1)
        XCTAssertEqual(core.calls.filter { $0 == "deregister" }.count, 1)
    }

    func testCancellingAtTheDeviceLimitSkipsDeregister() async {
        let core = FakeCore()
        core.loginResult = .failure(CoreError(message: "mobile: register device: device limit reached"))
        let store = SessionStore(core: core, tunnel: trackedTunnel(core), authenticator: FakeAuthenticator(.denied))
        await store.login("ABCD-EFGH")
        XCTAssertTrue(store.showDeviceLimit)
        await store.signOut()
        XCTAssertFalse(core.calls.contains("deregister"))
        XCTAssertEqual(Array(core.calls.suffix(3)), ["disconnect", "removeConfiguration", "logout"])
        XCTAssertFalse(store.signingOut)
        assertCleared(store)
    }

    func testWipeIsLocalAndClearsEverything() async throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        fixture.group.defaults.set(true, forKey: SharedPrefs.Key.killSwitch)
        fixture.standard.defaults.set("x", forKey: "someKey")
        try fixture.write("transport-memory.json")
        let core = FakeCore()
        let tunnel = trackedTunnel(core)
        let store = await signedInStore(core, tunnel: tunnel, wiper: fixture.wiper())
        let before = core.calls.count
        await store.wipe()
        XCTAssertEqual(Array(core.calls.dropFirst(before)), ["disconnect", "removeConfiguration", "wipe"])
        XCTAssertNil(fixture.group.defaults.object(forKey: SharedPrefs.Key.killSwitch))
        XCTAssertNil(fixture.standard.defaults.object(forKey: "someKey"))
        XCTAssertFalse(fixture.exists("transport-memory.json"))
        XCTAssertEqual(tunnel.removals, 1)
        assertCleared(store)
    }

    func testWipeCarriesOnWhenTheCoreFails() async throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        fixture.group.defaults.set(true, forKey: SharedPrefs.Key.lockdown)
        let core = FakeCore()
        let store = await signedInStore(core, tunnel: trackedTunnel(core), wiper: fixture.wiper())
        core.wipeResult = .failure(CoreError(message: "remove transport memory: permission denied"))
        await store.wipe()
        XCTAssertNil(fixture.group.defaults.object(forKey: SharedPrefs.Key.lockdown))
        XCTAssertFalse(core.calls.contains("deregister"))
        assertCleared(store)
    }

    func testWipeDoesNotWaitOnAStuckCore() async throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        fixture.group.defaults.set(true, forKey: SharedPrefs.Key.autoConnect)
        let fake = FakeCore()
        let core = GatedCore(fake)
        let store = SessionStore(
            core: core, tunnel: FakeTunnel(), wiper: fixture.wiper(),
            timeouts: SessionTimeouts(coreWipe: 0.1))
        let started = Date()
        await store.wipe()
        let elapsed = Date().timeIntervalSince(started)
        core.release()
        XCTAssertLessThan(elapsed, 4)
        XCTAssertNil(fixture.group.defaults.object(forKey: SharedPrefs.Key.autoConnect))
        XCTAssertNil(store.session)
    }
}
