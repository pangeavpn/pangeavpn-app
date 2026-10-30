import Foundation
import Observation
import os

@MainActor
@Observable
final class ServersStore {
    private(set) var regions: [Region] = []
    private(set) var selected: Server?
    private(set) var loading = false
    // Set only when a node was chosen by hand; otherwise each region connects to its lightest node.
    private(set) var pinnedServerId: String?
    private(set) var recentRegionKeys: [String] = []
    private(set) var rotationVisited: [String] = []
    var error: String?

    private let core: any CoreBridge
    private let defaults: UserDefaults
    // Bumped by reset, so a refresh still in flight for the old account is dropped.
    @ObservationIgnored private var generation = 0
    // A server the tunnel reported before the list had loaded; the next refresh selects it.
    @ObservationIgnored private var pendingFollow: String?

    private static let log = Logger(subsystem: "org.pangeavpn", category: "servers")

    init(core: any CoreBridge, defaults: UserDefaults = AppGroup.defaults) {
        self.core = core
        self.defaults = defaults
        recentRegionKeys = SharedPrefs.strings(SharedPrefs.Key.recentRegions, in: defaults)
        rotationVisited = SharedPrefs.strings(SharedPrefs.Key.rotationVisited, in: defaults)
        pinnedServerId = defaults.string(forKey: SharedPrefs.Key.pinnedServerId)
    }

    var selectedRegionKey: String? {
        selected.flatMap { Regions.region(of: $0.id, in: regions)?.key }
    }

    var slots: [Region] {
        slots(for: Transport.auto.rawValue)
    }

    func slots(for transport: String) -> [Region] {
        let ordered = Regions.orderByRecent(regions(for: transport), recent: recentRegionKeys)
        return Regions.slots(ordered, selectedKey: selectedRegionKey)
    }

    func regions(for transport: String) -> [Region] {
        Regions.supporting(transport, in: regions)
    }

    func server(id: String) -> Server? {
        for region in regions {
            if let found = region.servers.first(where: { $0.id == id }) { return found }
        }
        return nil
    }

    // An overlapping call would clear loading while the first one is still in flight.
    func refresh() async {
        guard !loading else { return }
        loading = true
        let started = generation
        let core = self.core
        do {
            let listed = try await CoreQueue.run { try core.listServers() }
            let last = await lastServerId()
            guard started == generation else { return }
            let preferred = [pendingFollow, selected?.id].compactMap { $0 }
                .first { id in listed.contains { $0.id == id } }
            let id = Regions.resolveSelection(visible: listed, previous: preferred ?? "", lastServerId: last)
            regions = Regions.group(listed)
            selected = id.isEmpty ? nil : listed.first { $0.id == id }
            pendingFollow = nil
            error = nil
        } catch {
            guard started == generation else { return }
            Self.log.error("server refresh failed: \(error.localizedDescription, privacy: .private)")
            self.error = error.localizedDescription
        }
        loading = false
    }

    // After sign-out, so the next account never sees the previous account's servers or history.
    func reset() {
        generation += 1
        regions = []
        selected = nil
        error = nil
        loading = false
        pendingFollow = nil
        recentRegionKeys = []
        rotationVisited = []
        pinnedServerId = nil
        let keys = [SharedPrefs.Key.recentRegions, SharedPrefs.Key.rotationVisited, SharedPrefs.Key.pinnedServerId]
        keys.forEach { defaults.removeObject(forKey: $0) }
    }

    func select(region: Region) async {
        guard !region.servers.isEmpty else { return }
        remember(region.key)
        await adopt(region.best, pinnedId: nil)
    }

    func select(server: Server, pinned: Bool) async {
        remember(Regions.key(of: server))
        await adopt(server, pinnedId: pinned ? server.id : nil)
    }

    // The tunnel landed elsewhere, on a fallback candidate; a pin naming another node is stale.
    func follow(serverId: String) async {
        guard let landed = server(id: serverId) else {
            pendingFollow = serverId
            return
        }
        pendingFollow = nil
        guard landed.id != selected?.id else { return }
        await adopt(landed, pinnedId: pinnedServerId == serverId ? serverId : nil)
    }

    // Persisted so on-demand and Settings-app starts land on the same server.
    func adopt(_ server: Server, pinnedId: String?) async {
        selected = server
        setPin(pinnedId)
        let core = self.core
        let id = server.id
        do {
            _ = try await CoreQueue.run(on: CoreQueue.settings) {
                try core.editConfig { $0.lastServerId = id }
            }
        } catch {
            Self.log.error("saving the server failed: \(error.localizedDescription, privacy: .private)")
            self.error = error.localizedDescription
        }
    }

    // A fixed transport stays on the chosen server; auto falls back as desktop does.
    func candidates(transport: String) -> [String] {
        guard let selected else { return [] }
        guard transport == Transport.auto.rawValue else { return [selected.id] }
        return Regions.candidates(from: selected.id, in: visibleServers(transport))
    }

    func rotation(transport: String) -> RotationPlan {
        guard let current = selected?.id else { return RotationPlan(plan: [], visited: rotationVisited) }
        let order = Regions.retryOrder(from: current, in: visibleServers(transport))
        return Rotation.plan(order: order, currentId: current, visited: rotationVisited)
    }

    func rotationPlan(transport: String) -> [Server] {
        rotation(transport: transport).plan.compactMap { server(id: $0) }
    }

    func recordRotation(_ rotation: RotationPlan, leaving: String, landedId: String?) {
        let next = Rotation.record(visited: rotation.visited, currentId: leaving, plan: rotation.plan, landedId: landedId)
        rotationVisited = next
        defaults.set(next, forKey: SharedPrefs.Key.rotationVisited)
    }

    private func visibleServers(_ transport: String) -> [Server] {
        regions.flatMap(\.servers).filter { $0.supports(transport) }
    }

    private func remember(_ key: String) {
        let next = Regions.promoteRecent(recentRegionKeys, key: key)
        recentRegionKeys = next
        defaults.set(next, forKey: SharedPrefs.Key.recentRegions)
    }

    private func setPin(_ id: String?) {
        pinnedServerId = id
        if let id {
            defaults.set(id, forKey: SharedPrefs.Key.pinnedServerId)
        } else {
            defaults.removeObject(forKey: SharedPrefs.Key.pinnedServerId)
        }
    }

    private func lastServerId() async -> String? {
        let core = self.core
        do {
            return try await CoreQueue.run { try core.config().lastServerId }
        } catch {
            Self.log.error("reading the last server failed: \(error.localizedDescription, privacy: .private)")
            return nil
        }
    }
}
