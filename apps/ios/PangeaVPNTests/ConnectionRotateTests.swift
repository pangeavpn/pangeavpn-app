import XCTest
@testable import PangeaVPN

@MainActor
final class ConnectionRotateTests: XCTestCase {
    private func visited(_ defaults: UserDefaults) -> [String] {
        SharedPrefs.strings(SharedPrefs.Key.rotationVisited, in: defaults)
    }

    func testRotationVisitsEveryServerOnceThenStartsOver() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "eu-2")

        await rig.store.rotate()
        XCTAssertEqual(rig.tunnel.switches.last, FakeTunnel.Switch(serverId: "eu-1", candidates: ["eu-1", "us-1", "ap-1"]))
        XCTAssertEqual(visited(defaults), ["eu-2", "eu-1"])
        XCTAssertEqual(rig.servers.pinnedServerId, "eu-1")

        await rig.store.rotate()
        XCTAssertEqual(rig.tunnel.switches.last?.candidates, ["us-1", "ap-1", "eu-2"])
        XCTAssertEqual(visited(defaults), ["eu-2", "eu-1", "us-1"])

        await rig.store.rotate()
        XCTAssertEqual(rig.tunnel.switches.last?.serverId, "ap-1")
        XCTAssertEqual(visited(defaults), ["eu-2", "eu-1", "us-1", "ap-1"])

        await rig.store.rotate()
        XCTAssertEqual(rig.tunnel.switches.last?.candidates, ["eu-2", "eu-1", "us-1"])
        XCTAssertEqual(visited(defaults), ["ap-1", "eu-2"])

        XCTAssertEqual(rig.tunnel.switches.map(\.serverId), ["eu-1", "us-1", "ap-1", "eu-2"])
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertEqual(rig.tunnel.disconnects, 0)
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
        XCTAssertEqual(rig.servers.pinnedServerId, "eu-2")
        XCTAssertEqual(rig.core.savedSettings.last?.lastServerId, "eu-2")
    }

    func testRotationResumesThePersistedCycle() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set(["eu-1"], forKey: SharedPrefs.Key.rotationVisited)
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "eu-2")
        await rig.store.rotate()
        XCTAssertEqual(rig.tunnel.switches, [FakeTunnel.Switch(serverId: "us-1", candidates: ["us-1", "ap-1", "eu-1"])])
        XCTAssertEqual(visited(defaults), ["eu-1", "eu-2", "us-1"])
    }

    func testAFailedRotationRestoresTheSelectionAndRemembersTheAttempts() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.store.activate(server: rig.server("eu-2"))
        rig.tunnel.switchResult = .failure(CoreError(message: "no route"))
        await rig.store.rotate()
        XCTAssertEqual(rig.store.error, "no route")
        XCTAssertEqual(rig.servers.selected?.id, "eu-2")
        XCTAssertEqual(rig.servers.pinnedServerId, "eu-2")
        XCTAssertEqual(rig.core.savedSettings.last?.lastServerId, "eu-2")
        XCTAssertEqual(visited(defaults), ["eu-2", "eu-1", "us-1", "ap-1"])
        XCTAssertFalse(rig.store.switching)
    }

    func testRotationMovesTheRecentRegion() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.connected(to: "us-1")
        await rig.store.rotate()
        XCTAssertEqual(rig.tunnel.switches.map(\.serverId), ["ap-1"])
        XCTAssertEqual(rig.servers.recentRegionKeys.first, "ap")
    }

    func testCanRotateOnlyWhileConnectedWithSomewhereElseToGo() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        XCTAssertFalse(rig.store.canRotate)
        for state in [TunnelState.connecting, .disconnecting, .error] {
            rig.tunnel.status = ConnectionFixtures.status("eu-2", state)
            XCTAssertFalse(rig.store.canRotate, "\(state)")
        }
        rig.tunnel.status = ConnectionFixtures.status("eu-2")
        XCTAssertTrue(rig.store.canRotate)
        rig.core.subscriptionResult = .success(Subscription(status: "expired", renews: false, expiresAt: nil, entitled: false))
        await rig.session.loadSubscription()
        XCTAssertFalse(rig.store.canRotate)
    }

    func testASingleServerCannotRotate() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults, list: [ConnectionFixtures.node("eu-1", load: 5)])
        try await rig.connected(to: "eu-1")
        XCTAssertFalse(rig.store.canRotate)
        await rig.store.rotate()
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
    }

    func testRotateWhileDisconnectedDoesNothing() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        try await rig.servers.select(server: rig.server("eu-2"), pinned: false)
        await rig.store.rotate()
        XCTAssertTrue(rig.tunnel.switches.isEmpty)
        XCTAssertTrue(rig.tunnel.requests.isEmpty)
        XCTAssertTrue(visited(defaults).isEmpty)
    }
}
