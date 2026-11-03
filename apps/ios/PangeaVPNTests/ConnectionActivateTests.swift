import XCTest
@testable import PangeaVPN

private typealias F = ConnectionFixtures

@MainActor
final class ConnectionActivateTests: XCTestCase {
    func testTappingARegionWhileDisconnectedConnectsToItsLightestNode() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertEqual(rig.tunnel.requests, [FakeTunnel.Request(
            serverId: "eu-2", candidates: ["eu-2", "eu-1", "us-1", "ap-1"], profile: rig.settings.tunnelProfile)])
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
        XCTAssertNil(rig.servers.pinnedServerId)
        XCTAssertEqual(rig.servers.recentRegionKeys, ["eu"])
        XCTAssertEqual(rig.core.savedSettings.last?.lastServerId, "eu-2")
        XCTAssertFalse(rig.store.busy)
    }

    func testTappingARegionAfterAnErrorReleasesThenConnects() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "us-1", state: .error)
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertEqual(rig.tunnel.disconnects, 1)
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-2"])
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
    }

    func testTappingAnotherRegionWhileUpSwitchesInPlace() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        for state in [TunnelState.connected, .connecting] {
            let rig = await makeConnectionRig(defaults)
            try await rig.connected(to: "us-1", state: state)
            try await rig.store.activate(region: rig.region("eu"))
            XCTAssertEqual(rig.tunnel.switches, [
                FakeTunnel.Switch(serverId: "eu-2", candidates: ["eu-2", "eu-1", "us-1", "ap-1"]),
            ], "\(state)")
            XCTAssertTrue(rig.tunnel.requests.isEmpty, "\(state)")
            XCTAssertEqual(rig.tunnel.disconnects, 0, "\(state)")
            XCTAssertEqual(rig.servers.selected?.id, "eu-2", "\(state)")
            XCTAssertEqual(rig.core.savedSettings.last?.lastServerId, "eu-2", "\(state)")
            XCTAssertEqual(rig.store.status.serverId, "eu-2", "\(state)")
            XCTAssertFalse(rig.store.switching, "\(state)")
            XCTAssertNil(rig.store.error, "\(state)")
        }
    }

    func testTappingTheRegionAlreadyInUseDoesNothingToTheTunnel() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "eu-2")
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertEqual(rig.tunnel.disconnects, 0)
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
    }

    func testTappingWhileDisconnectingOnlySelects() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "us-1", state: .disconnecting)
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
    }

    func testTappingANodePinsItAndSwitchesToIt() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "eu-2")
        try await rig.store.activate(server: rig.server("eu-1"))
        XCTAssertEqual(rig.tunnel.switches, [
            FakeTunnel.Switch(serverId: "eu-1", candidates: ["eu-1", "eu-2", "us-1", "ap-1"]),
        ])
        XCTAssertEqual(rig.servers.pinnedServerId, "eu-1")
        XCTAssertEqual(defaults.string(forKey: SharedPrefs.Key.pinnedServerId), "eu-1")
    }

    func testTappingANodeWhileDisconnectedPinsAndConnects() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.store.activate(server: rig.server("eu-1"))
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-1"])
        XCTAssertEqual(rig.servers.pinnedServerId, "eu-1")
    }

    func testTappingTheRegionDropsAPin() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.store.activate(server: rig.server("eu-1"))
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertEqual(rig.tunnel.switches.map(\.serverId), ["eu-2"])
        XCTAssertNil(rig.servers.pinnedServerId)
        XCTAssertNil(defaults.string(forKey: SharedPrefs.Key.pinnedServerId))
    }

    func testAFailedSwitchShowsTheErrorAndKeepsTheServerInUse() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "us-1")
        rig.tunnel.switchResult = .failure(CoreError(message: "hub refused the move"))
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertEqual(rig.store.error, "hub refused the move")
        XCTAssertEqual(rig.servers.selected?.id, "us-1")
        XCTAssertEqual(rig.core.savedSettings.last?.lastServerId, "us-1")
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertFalse(rig.store.switching)
    }

    func testANotRunningTunnelFallsBackToAStart() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = try await scriptedRig(defaults, failing: TunnelSwitchError.notRunning)
        try await rig.store.activate(region: XCTUnwrap(rig.servers.regions.first { $0.key == "eu" }))
        XCTAssertEqual(rig.tunnel.calls, ["switch(eu-2)", "connect(eu-2)"])
        XCTAssertNil(rig.store.error)
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
    }

    func testARefusedSwitchFallsBackToAFreshStart() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = try await scriptedRig(defaults, failing: TunnelSwitchError.rejected)
        try await rig.store.activate(region: XCTUnwrap(rig.servers.regions.first { $0.key == "eu" }))
        XCTAssertEqual(rig.tunnel.calls, ["switch(eu-2)", "disconnect", "connect(eu-2)"])
        XCTAssertNil(rig.store.error)
        XCTAssertEqual(rig.store.status.serverId, "eu-2")
    }

    // A disconnect would turn Lockdown's Connect On Demand off and let traffic out.
    func testARefusedSwitchUnderLockdownKeepsTheTunnelUp() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set(true, forKey: SharedPrefs.Key.lockdown)
        let rig = try await scriptedRig(defaults, failing: TunnelSwitchError.rejected)
        try await rig.store.activate(region: XCTUnwrap(rig.servers.regions.first { $0.key == "eu" }))
        XCTAssertEqual(rig.tunnel.calls, ["switch(eu-2)"])
        XCTAssertNotNil(rig.store.error)
        XCTAssertEqual(rig.store.status.serverId, "us-1")
        XCTAssertEqual(rig.servers.selected?.id, "us-1")
    }

    func testOtherSwitchErrorsAreShown() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = try await scriptedRig(defaults, failing: CoreError(message: "no route"))
        try await rig.store.activate(region: XCTUnwrap(rig.servers.regions.first { $0.key == "eu" }))
        XCTAssertEqual(rig.tunnel.calls, ["switch(eu-2)"])
        XCTAssertEqual(rig.store.error, "no route")
        XCTAssertEqual(rig.servers.selected?.id, "us-1")
    }

    func testActivateIsIgnoredWhileASwitchIsInFlight() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "us-1")
        rig.core.configDelay = 0.2
        let eu = try rig.region("eu")
        let first = Task { await rig.store.activate(region: eu) }
        let started = await eventually { rig.store.switching }
        XCTAssertTrue(started)
        try await rig.store.activate(region: rig.region("ap"))
        await first.value
        XCTAssertEqual(rig.tunnel.switches.map(\.serverId), ["eu-2"])
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
    }

    func testActivateIsIgnoredWhileAConnectIsInFlight() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.core.configDelay = 0.2
        let eu = try rig.region("eu")
        let first = Task { await rig.store.activate(region: eu) }
        let started = await eventually { rig.store.busy }
        XCTAssertTrue(started)
        try await rig.store.activate(region: rig.region("us"))
        await first.value
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-2"])
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
    }

    func testAnExpiredAccountSelectsButDoesNotConnect() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.core.subscriptionResult = .success(Subscription(status: "expired", renews: false, expiresAt: nil, entitled: false))
        await rig.session.loadSubscription()
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
        XCTAssertEqual(rig.store.error, TunnelStartError.subscriptionExpired.localizedDescription)
    }

    func testActivatingClosesTheRegionPicker() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.store.showServerSheet = true
        try await rig.store.activate(region: rig.region("us"))
        XCTAssertFalse(rig.store.showServerSheet)
    }

    func testAFixedTransportPicksTheRegionsBestNodeThatCarriesIt() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let list = [F.node("eu-1", load: 30, transports: F.reality), F.node("eu-2", load: 10)]
        let rig = await makeConnectionRig(defaults, list: list)
        await rig.settings.setPreferredTransport(.reality)
        try await rig.store.activate(region: rig.region("eu"))
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-1"])
        XCTAssertEqual(rig.tunnel.requests.map(\.candidates), [["eu-1"]])
    }

    func testTheSelectionFollowsAFallbackLandingWithoutRepinning() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.store.activate(server: rig.server("eu-1"))
        rig.tunnel.status = F.status("eu-2")
        let followed = await eventually { rig.servers.selected?.id == "eu-2" }
        XCTAssertTrue(followed)
        XCTAssertNil(rig.servers.pinnedServerId)
        let saved = await eventually { rig.core.savedSettings.last?.lastServerId == "eu-2" }
        XCTAssertTrue(saved)
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
    }

    func testRouteChangeMovesInPlaceToTheCurrentServer() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "eu-2")
        await rig.store.routeChanged()
        XCTAssertEqual(rig.tunnel.switches, [
            FakeTunnel.Switch(serverId: "eu-2", candidates: ["eu-2", "eu-1", "us-1", "ap-1"]),
        ])
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertFalse(rig.store.switching)
    }

    func testRouteChangeLeavesAnIdleTunnelAlone() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        for state in [TunnelState.disconnected, .connecting, .error] {
            rig.tunnel.status = F.status("eu-2", state)
            await rig.store.routeChanged()
        }
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
    }

    private struct ScriptedRig {
        let store: ConnectionStore
        let tunnel: ScriptedTunnel
        let servers: ServersStore
    }

    // The switch error type is not a CoreError, so these use a tunnel that can throw it.
    private func scriptedRig(_ defaults: UserDefaults, failing switchError: Error) async throws -> ScriptedRig {
        let core = FakeCore()
        core.serversResult = .success(F.fleet)
        let tunnel = ScriptedTunnel()
        tunnel.switchError = switchError
        let servers = ServersStore(core: core, defaults: defaults)
        let settings = SettingsStore(core: core, defaults: defaults)
        let session = SessionStore(core: core, tunnel: tunnel)
        let store = ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session,
                                    sleeper: { _ in throw CancellationError() })
        await servers.refresh()
        try await servers.select(server: XCTUnwrap(servers.server(id: "us-1")), pinned: false)
        tunnel.status = F.status("us-1")
        return ScriptedRig(store: store, tunnel: tunnel, servers: servers)
    }
}
