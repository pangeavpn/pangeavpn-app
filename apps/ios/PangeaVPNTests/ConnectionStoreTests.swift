import XCTest
@testable import PangeaVPN

private let pick = Server(id: "eu-1", name: "Frankfurt", region: "eu", country: "DE", load: 10)

@MainActor
final class ConnectionStoreTests: XCTestCase {
    private struct Rig {
        let tunnel: FakeTunnel
        let servers: ServersStore
        let settings: SettingsStore
        let store: ConnectionStore
    }

    private func makeRig(defaults: UserDefaults) -> Rig {
        let core = FakeCore()
        let tunnel = FakeTunnel()
        let servers = ServersStore(core: core)
        let settings = SettingsStore(core: core, defaults: defaults)
        let store = ConnectionStore(tunnel: tunnel, servers: servers, settings: settings)
        return Rig(tunnel: tunnel, servers: servers, settings: settings, store: store)
    }

    func testToggleWithoutSelectionOpensSheet() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = makeRig(defaults: defaults)
        await rig.store.toggle()
        XCTAssertTrue(rig.store.showServerSheet)
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
    }

    func testToggleConnectsWithKillSwitchAndAllowLan() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = makeRig(defaults: defaults)
        await rig.servers.select(pick)
        rig.settings.setKillSwitch(true)
        await rig.settings.setAllowLan(true)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.requests, [FakeTunnel.Request(serverId: "eu-1", killSwitch: true, allowLan: true)])
        XCTAssertEqual(rig.store.status.state, .connected)
        XCTAssertNotNil(rig.store.connectedDate)
        XCTAssertFalse(rig.store.showServerSheet)
        XCTAssertNil(rig.store.error)
    }

    func testToggleWhileConnectedOrConnectingDisconnects() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        for state in [TunnelState.connected, .connecting] {
            let rig = makeRig(defaults: defaults)
            await rig.servers.select(pick)
            rig.tunnel.status.state = state
            XCTAssertEqual(rig.store.status.state, state)
            await rig.store.toggle()
            XCTAssertEqual(rig.tunnel.disconnects, 1, "\(state)")
            XCTAssertTrue(rig.tunnel.requests.isEmpty, "\(state)")
            XCTAssertEqual(rig.store.status.state, .disconnected, "\(state)")
        }
    }

    func testConnectFailureSetsError() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = makeRig(defaults: defaults)
        await rig.servers.select(pick)
        rig.tunnel.connectResult = .failure(CoreError(message: "permission denied"))
        await rig.store.toggle()
        XCTAssertEqual(rig.store.error, "permission denied")
        XCTAssertEqual(rig.store.status.state, .disconnected)
    }
}
