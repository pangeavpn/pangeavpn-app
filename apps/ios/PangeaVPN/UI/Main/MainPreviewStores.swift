#if DEBUG
import SwiftUI

enum MainPreviewData {
    static let servers: [Server] = [
        Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 41, multihop: true),
        Server(id: "uk-lon-2", name: "London", region: "eu", country: "GB", load: 61, multihop: true),
        Server(id: "nl-ams-1", name: "Amsterdam", region: "eu", country: "NL", load: 33, multihop: true),
        Server(id: "nl-ams-2", name: "Amsterdam", region: "eu", country: "NL", load: 52),
        Server(id: "us-nyc-1", name: "New York", region: "us", country: "US", load: 73),
        Server(id: "sg-1", name: "Singapore", region: "ap", country: "SG", load: 35),
    ]
    static let active = Subscription(status: "active", renews: true, expiresAt: "2026-12-01T00:00:00Z", entitled: true)
    static let pastDue = Subscription(status: "past_due", renews: false, expiresAt: "2026-11-02T00:00:00Z", entitled: true)
    static let expired = Subscription(status: "expired", renews: false, expiresAt: "2026-10-20T00:00:00Z", entitled: false)

    static func status(_ state: TunnelState, serverId: String = "uk-lon-1", serverName: String = "London",
                       detail: String = "", bytesIn: Int64 = 0, bytesOut: Int64 = 0,
                       transport: String? = nil, postQuantum: Bool = false,
                       entryId: String = "", entryName: String = "") -> TunnelStatus {
        TunnelStatus(state: state, detail: detail, bytesIn: bytesIn, bytesOut: bytesOut,
                     serverId: serverId, serverName: serverName, activeTransport: transport,
                     lastHandshake: state == .connected ? Int64(Date().timeIntervalSince1970) : 0,
                     postQuantum: postQuantum, entryServerId: entryId, entryServerName: entryName)
    }
}

// Fake-backed stores for the main screen's previews, on a throwaway defaults suite.
@MainActor
final class MainPreviewStores {
    let core: FakeCore
    let tunnel: FakeTunnel
    let session: SessionStore
    let servers: ServersStore
    let settings: SettingsStore
    let connection: ConnectionStore
    let toasts: ToastCenter

    init(status: TunnelStatus = .disconnected, connectedFor: TimeInterval? = nil,
         subscription: Subscription? = MainPreviewData.active, servers list: [Server] = MainPreviewData.servers) {
        let suite = "org.pangeavpn.preview.main"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        var config = AppSettings()
        config.lastServerId = status.serverId.isEmpty ? (list.first?.id ?? "") : status.serverId
        let fakeCore = FakeCore()
        fakeCore.configResult = .success(config)
        fakeCore.serversResult = .success(list)
        fakeCore.subscriptionResult = .success(subscription)
        fakeCore.restoreResult = .success(Session(email: "demo@example.org", name: "Demo", servers: []))
        let fakeTunnel = FakeTunnel()
        fakeTunnel.status = status
        fakeTunnel.connectedDate = connectedFor.map { Date().addingTimeInterval(-$0) }
        let sessionStore = SessionStore(core: fakeCore, tunnel: fakeTunnel)
        let serversStore = ServersStore(core: fakeCore, defaults: defaults)
        let settingsStore = SettingsStore(core: fakeCore, defaults: defaults)
        core = fakeCore
        tunnel = fakeTunnel
        session = sessionStore
        servers = serversStore
        settings = settingsStore
        toasts = ToastCenter()
        connection = ConnectionStore(tunnel: fakeTunnel, servers: serversStore, settings: settingsStore, session: sessionStore)
    }

    func attach<V: View>(_ view: V) -> some View {
        view
            .environment(session)
            .environment(servers)
            .environment(settings)
            .environment(connection)
            .environment(toasts)
            .toastOverlay()
            .task { await self.load() }
    }

    func load() async {
        await session.restore()
        await settings.load()
        await servers.refresh()
    }
}
#endif
