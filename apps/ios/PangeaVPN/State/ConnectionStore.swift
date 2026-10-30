import Foundation
import Observation
import os

@MainActor
@Observable
final class ConnectionStore {
    typealias Sleeper = @MainActor (Duration) async throws -> Void

    private(set) var status = TunnelStatus.disconnected
    private(set) var connectedDate: Date?
    private(set) var busy = false
    private(set) var switching = false
    private(set) var progress: String?
    var showServerSheet = false
    var error: String?

    private let tunnel: any TunnelControlling
    private let servers: ServersStore
    private let settings: SettingsStore
    private let session: SessionStore
    private let sleeper: Sleeper
    @ObservationIgnored private var progressTask: Task<Void, Never>?
    // Set by a user stop, so an attempt it cut short is not reported as a failure.
    @ObservationIgnored private var stopRequested = false

    private static let log = Logger(subsystem: "org.pangeavpn", category: "connection")

    init(tunnel: any TunnelControlling, servers: ServersStore, settings: SettingsStore,
         session: SessionStore, sleeper: @escaping Sleeper = { try await Task.sleep(for: $0) }) {
        self.tunnel = tunnel
        self.servers = servers
        self.settings = settings
        self.session = session
        self.sleeper = sleeper
        sync()
        tunnel.onChange = { [weak self] in self?.sync() }
    }

    // Unknown counts as entitled; only the hub's explicit no blocks connecting.
    var entitled: Bool {
        session.subscription?.entitled != false
    }

    // Lockdown keeps the tunnel up until Lockdown itself is turned off.
    var lockdownBlocksDisconnect: Bool {
        settings.lockdown && Self.isUp(status.state)
    }

    var canRotate: Bool {
        status.state == .connected && !inFlight && entitled
            && !servers.rotationPlan(transport: transport).isEmpty
    }

    private var transport: String { settings.settings.preferredTransport }
    private var inFlight: Bool { busy || switching }

    // A tap while stopping is ignored, so it cannot queue a start behind the stop.
    func toggle() async {
        guard !busy else { return }
        let state = tunnel.status.state
        switch state {
        case .connected, .connecting:
            guard !settings.lockdown else { return }
            stopRequested = true
            await tunnel.disconnect()
        case .disconnecting:
            return
        case .disconnected, .error:
            guard !switching else { return }
            busy = true
            defer { busy = false }
            await connect(releasingFirst: state == .error)
        }
    }

    func activate(region: Region) async {
        let nodes = region.servers.filter { $0.supports(transport) }
        let usable = nodes.isEmpty ? region : Region(key: region.key, servers: nodes)
        guard !usable.servers.isEmpty else { return }
        await move(to: usable.best) { await self.servers.select(region: usable) }
    }

    func activate(server: Server) async {
        await move(to: server) { await self.servers.select(server: server, pinned: true) }
    }

    // Lands on the first server that answers and pins it; on failure the old pick comes back.
    func rotate() async {
        guard canRotate, let leaving = servers.selected else { return }
        let rotation = servers.rotation(transport: transport)
        guard let firstId = rotation.plan.first, let first = servers.server(id: firstId) else { return }
        let previousPin = servers.pinnedServerId
        switching = true
        defer { switching = false }
        stopRequested = false
        await servers.select(server: first, pinned: false)
        let moved = await moveTunnel(to: first.id, candidates: Array(rotation.plan.prefix(Regions.candidateLimit)))
        let cancelled = stopRequested
        let landedId = moved && !cancelled ? landing(in: rotation.plan, fallback: first.id) : nil
        if !cancelled {
            servers.recordRotation(rotation, leaving: leaving.id, landedId: landedId)
        }
        if let landedId, let landed = servers.server(id: landedId) {
            await servers.adopt(landed, pinnedId: landedId)
        } else {
            await servers.adopt(leaving, pinnedId: previousPin)
        }
    }

    func applyProfile() async {
        await tunnel.updateProfile(settings.tunnelProfile)
    }

    // Moving to the server it is already on makes the extension prepare the new route.
    func routeChanged() async {
        let now = tunnel.status
        guard now.state == .connected, !inFlight else { return }
        let current = now.serverId.isEmpty ? (servers.selected?.id ?? "") : now.serverId
        guard !current.isEmpty else { return }
        switching = true
        defer { switching = false }
        stopRequested = false
        _ = await moveTunnel(to: current, candidates: candidates(leading: current))
    }

    // A running tunnel moves in place; an idle one starts on the new pick.
    private func move(to target: Server, selecting select: () async -> Void) async {
        guard !inFlight else { return }
        showServerSheet = false
        let now = tunnel.status
        switch now.state {
        case .connected, .connecting:
            guard target.id != now.serverId else {
                await select()
                return
            }
            await switchInPlace(to: target, selecting: select)
        case .disconnected, .error:
            busy = true
            defer { busy = false }
            await select()
            await connect(releasingFirst: now.state == .error)
        case .disconnecting:
            await select()
        }
    }

    private func connect(releasingFirst: Bool) async {
        guard entitled else {
            error = TunnelStartError.subscriptionExpired.localizedDescription
            return
        }
        error = nil
        stopRequested = false
        // The extension reads allowLan from the core, so the profile must not use a default.
        if !settings.loaded { await settings.load() }
        guard settings.loaded else {
            error = settings.error
            return
        }
        guard let target = await connectTarget() else {
            showServerSheet = true
            return
        }
        // A failed tunnel can leave the drop rule up and sink this start; Lockdown wants it kept.
        if releasingFirst && !settings.lockdown { await tunnel.disconnect() }
        let leaving = tunnel.status.serverId
        if await start(target.id, candidates: servers.candidates(transport: transport)) {
            await adoptLanded(ignoring: leaving)
        }
    }

    // The pick if it carries the transport, else its region's best node that does, else a light node.
    private func connectTarget() async -> Server? {
        let visible = servers.regions(for: transport)
        if let current = servers.selected {
            if current.supports(transport) { return current }
            if let region = visible.first(where: { $0.key == Regions.key(of: current) }) {
                await servers.select(region: region)
                return servers.selected
            }
        }
        guard let pick = ServerPick.random(from: visible.flatMap(\.servers)) else { return nil }
        await servers.select(server: pick, pinned: false)
        return pick
    }

    private func start(_ serverId: String, candidates: [String]) async -> Bool {
        do {
            try await tunnel.connect(serverId: serverId, candidates: candidates, profile: settings.tunnelProfile)
            return true
        } catch {
            report(error, doing: "connect")
            return false
        }
    }

    private func switchInPlace(to target: Server, selecting select: () async -> Void) async {
        switching = true
        defer { switching = false }
        stopRequested = false
        let previous = servers.selected
        let previousPin = servers.pinnedServerId
        let leaving = tunnel.status.serverId
        await select()
        if await moveTunnel(to: target.id, candidates: candidates(leading: target.id)) {
            await adoptLanded(ignoring: leaving)
        } else {
            await settleFailedSwitch(previous: previous, previousPin: previousPin)
        }
    }

    // No running extension gets a plain start; one that refuses the message gets a fresh one.
    private func moveTunnel(to serverId: String, candidates: [String]) async -> Bool {
        error = nil
        do {
            try await tunnel.switchServer(serverId: serverId, candidates: candidates)
            return true
        } catch let refusal as TunnelSwitchError {
            Self.log.info("switch fell back to a start: \(String(describing: refusal), privacy: .public)")
            guard !stopRequested else { return false }
            if refusal == .rejected { await tunnel.disconnect() }
            return await start(serverId, candidates: candidates)
        } catch {
            report(error, doing: "switch")
            return false
        }
    }

    // Still up on another server, so the pick goes back to it and the screen tells the truth.
    private func settleFailedSwitch(previous: Server?, previousPin: String?) async {
        let now = tunnel.status
        guard now.state == .connected, !now.serverId.isEmpty, now.serverId != servers.selected?.id else { return }
        if let previous, previous.id == now.serverId {
            await servers.adopt(previous, pinnedId: previousPin)
        } else {
            await servers.follow(serverId: now.serverId)
        }
    }

    // The server being left is not a landing; the extension may report it until the move lands.
    private func adoptLanded(ignoring leaving: String) async {
        let now = tunnel.status
        guard now.state == .connected, !now.serverId.isEmpty, now.serverId != leaving,
              now.serverId != servers.selected?.id else { return }
        await servers.follow(serverId: now.serverId)
    }

    private func candidates(leading serverId: String) -> [String] {
        let rest = servers.candidates(transport: transport).filter { $0 != serverId }
        return Array(([serverId] + rest).prefix(Regions.candidateLimit))
    }

    private func landing(in plan: [String], fallback: String) -> String {
        let reported = tunnel.status.serverId
        return plan.contains(reported) ? reported : fallback
    }

    private func report(_ failure: Error, doing action: String) {
        Self.log.error("\(action, privacy: .public) failed: \(failure.localizedDescription, privacy: .private)")
        if !stopRequested { error = failure.localizedDescription }
    }

    private func sync() {
        let previous = status
        status = tunnel.status
        connectedDate = tunnel.connectedDate
        if status.state != previous.state { entered(status.state) }
        followTunnel(from: previous)
    }

    private func entered(_ state: TunnelState) {
        // A tunnel that got going makes the last connect error stale.
        if Self.isUp(state) { error = nil }
        if state == .connected { refreshSubscription() }
        trackProgress(state)
    }

    // Adopts where a running tunnel really is, such as after an on-demand start or a fallback.
    private func followTunnel(from previous: TunnelStatus) {
        let id = status.serverId
        let moved = previous.state != .connected || previous.serverId != id
        guard status.state == .connected, moved, !inFlight, !id.isEmpty, id != servers.selected?.id else { return }
        let servers = self.servers
        Task { await servers.follow(serverId: id) }
    }

    private func refreshSubscription() {
        let session = self.session
        Task { await session.loadSubscription() }
    }

    private func trackProgress(_ state: TunnelState) {
        progressTask?.cancel()
        progressTask = nil
        progress = nil
        guard state == .connecting else { return }
        let sleeper = self.sleeper
        progressTask = Task { [weak self] in
            for (wait, step) in zip(ConnectProgress.waits, ConnectProgress.steps) {
                do {
                    try await sleeper(wait)
                } catch {
                    return
                }
                guard !Task.isCancelled else { return }
                self?.progress = step.message
            }
        }
    }

    private static func isUp(_ state: TunnelState) -> Bool {
        state == .connected || state == .connecting
    }
}
