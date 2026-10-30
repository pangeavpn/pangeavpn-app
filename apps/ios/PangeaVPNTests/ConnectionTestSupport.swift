import XCTest
@testable import PangeaVPN

enum ConnectionFixtures {
    static let reality: Set<String> = Server.universalTransports.union(["reality"])

    static func node(_ id: String, load: Int? = nil, transports: Set<String> = Server.universalTransports) -> Server {
        Server(id: id, name: id, region: "r", country: "GB", load: load, transports: transports)
    }

    // Regions eu (eu-2 is lightest), us and ap, in that hub order.
    static var fleet: [Server] {
        [node("eu-1", load: 30), node("eu-2", load: 10), node("us-1", load: 20), node("ap-1", load: 40)]
    }

    static func status(_ serverId: String, _ state: TunnelState = .connected) -> TunnelStatus {
        TunnelStatus(state: state, detail: "", bytesIn: 0, bytesOut: 0,
                     serverId: serverId, serverName: serverId, activeTransport: nil)
    }
}

@MainActor
struct ConnectionRig {
    let core: FakeCore
    let tunnel: FakeTunnel
    let servers: ServersStore
    let settings: SettingsStore
    let session: SessionStore
    let store: ConnectionStore

    func region(_ key: String) throws -> Region {
        try XCTUnwrap(servers.regions.first { $0.key == key })
    }

    func server(_ id: String) throws -> Server {
        try XCTUnwrap(servers.server(id: id))
    }

    // Puts the tunnel on a server the way a finished connect would, selection included.
    func connected(to id: String, state: TunnelState = .connected) async throws {
        let target = try server(id)
        await servers.select(server: target, pinned: false)
        tunnel.status = ConnectionFixtures.status(id, state)
    }
}

@MainActor
func makeConnectionRig(_ defaults: UserDefaults, list: [Server] = ConnectionFixtures.fleet,
                       sleeper: ConnectionStore.Sleeper? = nil) async -> ConnectionRig {
    let core = FakeCore()
    core.serversResult = .success(list)
    let tunnel = FakeTunnel()
    let servers = ServersStore(core: core, defaults: defaults)
    let settings = SettingsStore(core: core, defaults: defaults)
    let session = SessionStore(core: core, tunnel: tunnel)
    let pause: ConnectionStore.Sleeper = sleeper ?? { _ in throw CancellationError() }
    let store = ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session, sleeper: pause)
    await servers.refresh()
    return ConnectionRig(core: core, tunnel: tunnel, servers: servers, settings: settings, session: session, store: store)
}

// Polls, because some store work runs in tasks the test cannot await directly.
@MainActor
func eventually(within timeout: Duration = .seconds(2), _ condition: () -> Bool) async -> Bool {
    let clock = ContinuousClock()
    let deadline = clock.now + timeout
    while !condition() {
        guard clock.now < deadline else { return false }
        try? await Task.sleep(for: .milliseconds(5))
    }
    return true
}

// Each sleep waits until the test fires it, so progress timing is driven step by step.
@MainActor
final class ManualSleeper {
    private(set) var requested: [Duration] = []
    private var pending: [CheckedContinuation<Void, Error>] = []

    var sleeper: ConnectionStore.Sleeper {
        { [unowned self] duration in try await self.sleep(duration) }
    }

    func sleep(_ duration: Duration) async throws {
        requested.append(duration)
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            pending.append(continuation)
        }
    }

    func fire() {
        guard !pending.isEmpty else { return }
        pending.removeFirst().resume()
    }

    func releaseAll() {
        let waiting = pending
        pending = []
        waiting.forEach { $0.resume(throwing: CancellationError()) }
    }
}

// Throws any error from a switch, which FakeTunnel's CoreError result cannot.
@MainActor
final class ScriptedTunnel: TunnelControlling {
    var status = TunnelStatus.disconnected {
        didSet { onChange?() }
    }
    var connectedDate: Date?
    var onDemandActive = false
    var onChange: (() -> Void)?
    var onAutoConnectDeclined: (() -> Void)?
    var switchError: Error?
    private(set) var calls: [String] = []

    func connect(serverId: String, candidates: [String], profile: TunnelProfile) async throws {
        calls.append("connect(\(serverId))")
        status = ConnectionFixtures.status(serverId)
    }

    func switchServer(serverId: String, candidates: [String]) async throws {
        calls.append("switch(\(serverId))")
        if let switchError { throw switchError }
        status = ConnectionFixtures.status(serverId)
    }

    func updateProfile(_ profile: TunnelProfile) async {
        calls.append("updateProfile")
    }

    func disconnect() async {
        calls.append("disconnect")
        status = .disconnected
    }

    func removeConfiguration() async {
        calls.append("removeConfiguration")
    }

    func extensionLogs() async -> String? { nil }
}
