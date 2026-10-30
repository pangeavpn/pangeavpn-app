import XCTest
@testable import PangeaVPN

private typealias F = ConnectionFixtures

@MainActor
final class ConnectionStoreTests: XCTestCase {
    private var expired: Subscription {
        Subscription(status: "expired", renews: false, expiresAt: nil, entitled: false)
    }

    func testToggleWithNoServersOpensTheRegionPicker() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults, list: [])
        await rig.store.toggle()
        XCTAssertTrue(rig.store.showServerSheet)
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertFalse(rig.store.busy)
    }

    func testToggleConnectsTheSelectionWithProfileAndCandidates() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        rig.settings.setKillSwitch(true)
        await rig.settings.setAllowLan(true)
        await rig.store.toggle()
        let profile = TunnelProfile(strictKillSwitch: true, allowLan: true, autoConnect: true, lockdown: false)
        XCTAssertEqual(rig.tunnel.requests, [
            FakeTunnel.Request(serverId: "eu-2", candidates: ["eu-2", "eu-1", "us-1", "ap-1"], profile: profile),
        ])
        XCTAssertEqual(rig.store.status.state, .connected)
        XCTAssertNotNil(rig.store.connectedDate)
        XCTAssertFalse(rig.store.showServerSheet)
        XCTAssertNil(rig.store.error)
        XCTAssertFalse(rig.store.busy)
    }

    func testToggleWithNothingPickedChoosesALightlyLoadedServer() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let list = [F.node("de-1", load: 90), F.node("nl-1", load: 10), F.node("fr-1", load: 95)]
        let rig = await makeConnectionRig(defaults, list: list)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["nl-1"])
        XCTAssertEqual(rig.servers.selected?.id, "nl-1")
    }

    func testToggleWhileConnectedOrConnectingDisconnects() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        for state in [TunnelState.connected, .connecting] {
            let rig = await makeConnectionRig(defaults)
            try await rig.connected(to: "eu-2", state: state)
            XCTAssertEqual(rig.store.status.state, state)
            await rig.store.toggle()
            XCTAssertEqual(rig.tunnel.disconnects, 1, "\(state)")
            XCTAssertTrue(rig.tunnel.requests.isEmpty, "\(state)")
            XCTAssertEqual(rig.store.status.state, .disconnected, "\(state)")
        }
    }

    func testLockdownKeepsARunningTunnelUp() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        for state in [TunnelState.connected, .connecting] {
            let rig = await makeConnectionRig(defaults)
            rig.settings.setLockdown(true)
            try await rig.connected(to: "eu-2", state: state)
            XCTAssertTrue(rig.store.lockdownBlocksDisconnect, "\(state)")
            await rig.store.toggle()
            XCTAssertEqual(rig.tunnel.disconnects, 0, "\(state)")
            XCTAssertEqual(rig.store.status.state, state)
            rig.settings.setLockdown(false)
            XCTAssertFalse(rig.store.lockdownBlocksDisconnect, "\(state)")
        }
    }

    func testLockdownDoesNotBlockWhileDisconnected() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.settings.setLockdown(true)
        XCTAssertFalse(rig.store.lockdownBlocksDisconnect)
    }

    func testToggleWhileDisconnectingDoesNothing() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "eu-2", state: .disconnecting)
        await rig.store.toggle()
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertEqual(rig.tunnel.disconnects, 0)
        XCTAssertFalse(rig.store.showServerSheet)
        XCTAssertEqual(rig.store.status.state, .disconnecting)
    }

    func testToggleLoadsSettingsBeforeConnecting() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        var stored = AppSettings()
        stored.allowLan = true
        rig.core.configResult = .success(stored)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.requests.map(\.profile.allowLan), [true])
        XCTAssertTrue(rig.settings.loaded)
    }

    func testSettingsLoadFailureBlocksTheConnect() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        rig.core.configResult = .failure(CoreError(message: "mobile: config unreadable"))
        await rig.store.toggle()
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertEqual(rig.store.error, "mobile: config unreadable")
        XCTAssertFalse(rig.store.busy)
    }

    func testConnectFailureSetsError() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        rig.tunnel.connectResult = .failure(CoreError(message: "permission denied"))
        await rig.store.toggle()
        XCTAssertEqual(rig.store.error, "permission denied")
        XCTAssertEqual(rig.store.status.state, .disconnected)
    }

    func testRetryFromAnErrorReleasesTheTunnelBeforeConnecting() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "eu-2", state: .error)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.disconnects, 1)
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-2"])
        XCTAssertEqual(rig.store.status.state, .connected)
        XCTAssertFalse(rig.store.busy)
    }

    func testRetryUnderLockdownKeepsTheTunnelHeld() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.settings.setLockdown(true)
        try await rig.connected(to: "eu-2", state: .error)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.disconnects, 0)
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-2"])
        XCTAssertEqual(rig.tunnel.requests.first?.profile.lockdown, true)
    }

    func testConnectFromDisconnectedDoesNotReleaseFirst() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.disconnects, 0)
        XCTAssertEqual(rig.tunnel.requests.count, 1)
    }

    func testConnectErrorClearsOnceTheTunnelGetsGoing() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        for state in [TunnelState.connecting, .connected] {
            let rig = await makeConnectionRig(defaults)
            try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
            rig.tunnel.connectResult = .failure(CoreError(message: "permission denied"))
            await rig.store.toggle()
            rig.tunnel.status.state = .disconnected
            XCTAssertEqual(rig.store.error, "permission denied", "\(state)")
            rig.tunnel.status.state = state
            XCTAssertNil(rig.store.error, "\(state)")
        }
    }

    func testAnExpiredAccountCannotConnect() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.core.subscriptionResult = .success(expired)
        await rig.session.loadSubscription()
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        XCTAssertFalse(rig.store.entitled)
        await rig.store.toggle()
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertEqual(rig.store.error, TunnelStartError.subscriptionExpired.localizedDescription)
        XCTAssertFalse(rig.store.busy)
    }

    func testOnlyAnExplicitNoCountsAsNotEntitled() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        XCTAssertTrue(rig.store.entitled)
        rig.core.subscriptionResult = .success(Subscription(status: "active", renews: true, expiresAt: nil))
        await rig.session.loadSubscription()
        XCTAssertTrue(rig.store.entitled)
        rig.core.subscriptionResult = .success(Subscription(status: "active", renews: true, expiresAt: nil, entitled: true))
        await rig.session.loadSubscription()
        XCTAssertTrue(rig.store.entitled)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-2"])
    }

    func testEachConnectRefreshesTheSubscriptionOnce() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.core.subscriptionResult = .success(expired)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        let lookups = { rig.core.calls.filter { $0 == "subscription" }.count }
        await rig.store.toggle()
        let refreshed = await eventually { !rig.store.entitled }
        XCTAssertTrue(refreshed)
        XCTAssertEqual(lookups(), 1)
        rig.tunnel.status = F.status("eu-2")
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(lookups(), 1, "a status update without a new connect is not a new connect")
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.disconnects, 1)
        rig.tunnel.status = F.status("eu-2")
        let again = await eventually { lookups() == 2 }
        XCTAssertTrue(again)
    }

    func testFixedTransportConnectsOnlyTheSelection() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let list = [F.node("eu-1", load: 30, transports: F.reality), F.node("us-1", load: 20, transports: F.reality)]
        let rig = await makeConnectionRig(defaults, list: list)
        await rig.settings.setPreferredTransport(.reality)
        try await rig.servers.select(server: rig.server("eu-1"), pinned: false)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.requests.map(\.candidates), [["eu-1"]])
    }

    func testFixedTransportReplacesAPickThatCannotCarryIt() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let list = [F.node("eu-1", load: 30, transports: F.reality), F.node("eu-2", load: 10), F.node("us-1", load: 5)]
        let rig = await makeConnectionRig(defaults, list: list)
        await rig.settings.setPreferredTransport(.reality)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        await rig.store.toggle()
        XCTAssertEqual(rig.tunnel.requests.map(\.serverId), ["eu-1"])
        XCTAssertEqual(rig.servers.selected?.id, "eu-1")
    }

    func testApplyProfilePushesTheCurrentProfile() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.settings.setLockdown(true)
        rig.settings.setKillSwitch(true)
        await rig.store.applyProfile()
        XCTAssertEqual(rig.tunnel.profiles, [rig.settings.tunnelProfile])
        XCTAssertEqual(rig.tunnel.profiles.first?.lockdown, true)
        XCTAssertEqual(rig.tunnel.profiles.first?.strictKillSwitch, true)
    }

    func testAutoConnectFollowsASwitchOffInIOSSettings() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        XCTAssertTrue(rig.settings.autoConnect)
        rig.tunnel.onAutoConnectDeclined?()
        XCTAssertFalse(rig.settings.autoConnect)
        XCTAssertFalse(rig.settings.tunnelProfile.autoConnect)
    }

    func testLockdownKeepsAutoConnectThroughASwitchOff() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.settings.setLockdown(true)
        rig.tunnel.onAutoConnectDeclined?()
        XCTAssertTrue(rig.settings.autoConnect)
    }
}
