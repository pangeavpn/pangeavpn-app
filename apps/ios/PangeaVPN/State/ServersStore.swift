import Foundation
import Observation
import os

@MainActor
@Observable
final class ServersStore {
    private(set) var regions: [Region] = []
    private(set) var selected: Server?
    private(set) var loading = false
    var error: String?

    private let core: any CoreBridge

    private static let log = Logger(subsystem: "org.pangeavpn", category: "servers")

    init(core: any CoreBridge) {
        self.core = core
    }

    var selectedRegionKey: String? {
        selected.flatMap { Regions.region(of: $0.id, in: regions)?.key }
    }

    var slots: [Region] {
        Regions.slots(regions, selectedKey: selectedRegionKey)
    }

    func refresh() async {
        loading = true
        let core = self.core
        do {
            let servers = try await CoreQueue.run { try core.listServers() }
            let last = await lastServerId()
            let id = Regions.resolveSelection(visible: servers, previous: selected?.id ?? "", lastServerId: last)
            regions = Regions.group(servers)
            selected = id.isEmpty ? nil : servers.first(where: { $0.id == id })
            error = nil
        } catch {
            Self.log.error("server refresh failed: \(error.localizedDescription, privacy: .public)")
            self.error = error.localizedDescription
        }
        loading = false
    }

    // Persisted so the extension can fall back to it when started from the Settings app.
    func select(_ server: Server) async {
        selected = server
        let core = self.core
        let id = server.id
        do {
            _ = try await CoreQueue.run(on: CoreQueue.settings) {
                try core.editConfig { $0.lastServerId = id }
            }
        } catch {
            Self.log.error("saving the server failed: \(error.localizedDescription, privacy: .public)")
            self.error = error.localizedDescription
        }
    }

    private func lastServerId() async -> String? {
        let core = self.core
        do {
            return try await CoreQueue.run { try core.config().lastServerId }
        } catch {
            Self.log.error("reading the last server failed: \(error.localizedDescription, privacy: .public)")
            return nil
        }
    }
}
