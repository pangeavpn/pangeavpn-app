import Foundation
import Observation

@MainActor
@Observable
final class ConnectionStore {
    private(set) var status = TunnelStatus.disconnected
    private(set) var connectedDate: Date?
    private(set) var busy = false
    var showServerSheet = false
    var error: String?

    private let tunnel: any TunnelControlling
    private let servers: ServersStore
    private let preferences: SettingsStore

    init(tunnel: any TunnelControlling, servers: ServersStore, settings: SettingsStore) {
        self.tunnel = tunnel
        self.servers = servers
        self.preferences = settings
        sync()
        tunnel.onChange = { [weak self] in self?.sync() }
    }

    // A tap while stopping is ignored, so it cannot queue a start behind the stop.
    func toggle() async {
        guard !busy else { return }
        switch tunnel.status.state {
        case .connected, .connecting:
            await tunnel.disconnect()
        case .disconnecting:
            return
        case .disconnected, .error:
            await connectSelected()
        }
    }

    private func connectSelected() async {
        guard let server = servers.selected else {
            showServerSheet = true
            return
        }
        busy = true
        defer { busy = false }
        error = nil
        // The extension reads allowLan from the core, so the NE flag must not use a default.
        if !preferences.loaded { await preferences.load() }
        guard preferences.loaded else {
            error = preferences.error
            return
        }
        do {
            try await tunnel.connect(
                serverId: server.id,
                killSwitch: preferences.killSwitch,
                allowLan: preferences.settings.allowLan)
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func sync() {
        status = tunnel.status
        connectedDate = tunnel.connectedDate
    }
}
