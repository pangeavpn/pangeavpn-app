import SwiftUI
@testable import PangeaVPN

enum GalleryData {
    static let servers: [Server] = [
        Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 22),
        Server(id: "uk-lon-2", name: "London", region: "eu", country: "GB", load: 61),
        Server(id: "de-fra-1", name: "Frankfurt", region: "eu", country: "DE", load: 48),
        Server(id: "nl-ams-1", name: "Amsterdam", region: "eu", country: "NL", load: 15),
        Server(id: "us-nyc-1", name: "New York", region: "us", country: "US", load: 73),
        Server(id: "us-lax-1", name: "Los Angeles", region: "us", country: "US", load: nil),
        Server(id: "ca-tor-1", name: "Toronto", region: "us", country: "CA", load: 30),
        Server(id: "sg-1", name: "Singapore", region: "ap", country: "SG", load: 35),
        Server(id: "jp-tyo-1", name: "Tokyo", region: "ap", country: "JP", load: 88),
    ]
    static let session = Session(email: "demo@example.org", name: "Demo", servers: [])
    static let active = Subscription(status: "active", renews: true, expiresAt: "2026-12-01")
    static let pastDue = Subscription(status: "past_due", renews: false, expiresAt: "2026-10-01")
    static let devices = [
        Device(id: "9f2c1a7e-44b0", friendlyName: "Work laptop", createdAt: "2026-09-01T10:00:00Z", status: "active"),
        Device(id: "b81d03c5-7a21", friendlyName: "Pixel 8", createdAt: "2026-09-12T08:30:00Z", status: "active"),
        Device(id: "c0ffee00-1234", friendlyName: nil, createdAt: "2026-10-02T19:45:00Z", status: "active"),
    ]

    static func status(_ state: TunnelState, detail: String = "", bytesIn: Int64 = 0,
                       bytesOut: Int64 = 0, transport: String? = nil) -> TunnelStatus {
        TunnelStatus(state: state, detail: detail, bytesIn: bytesIn, bytesOut: bytesOut,
                     serverId: "us-nyc-1", serverName: "New York", activeTransport: transport)
    }
}

// The preview fakes, loaded before a screen mounts, so its own .task finds the data already there.
@MainActor
final class GalleryStores {
    let core: FakeCore
    let tunnel: FakeTunnel
    let session: SessionStore
    let servers: ServersStore
    let settings: SettingsStore
    let connection: ConnectionStore
    private let connectedFor: TimeInterval?
    private let defaults: UserDefaults
    private let suite: String

    init(
        status: TunnelStatus = .disconnected,
        connectedFor: TimeInterval? = nil,
        subscription: Subscription? = nil,
        servers list: [Server] = GalleryData.servers
    ) throws {
        let test = try makeTestDefaults()
        defaults = test.defaults
        suite = test.suite
        self.connectedFor = connectedFor
        let core = FakeCore()
        var config = AppSettings()
        config.lastServerId = status.serverId.isEmpty ? (list.first?.id ?? "") : status.serverId
        core.configResult = .success(config)
        core.serversResult = .success(list)
        core.subscriptionResult = .success(subscription)
        core.restoreResult = .success(GalleryData.session)
        core.devicesResult = .success(GalleryData.devices)
        let tunnel = FakeTunnel()
        tunnel.status = status
        let servers = ServersStore(core: core)
        let settings = SettingsStore(core: core, defaults: test.defaults)
        self.core = core
        self.tunnel = tunnel
        self.servers = servers
        self.settings = settings
        session = SessionStore(core: core, tunnel: tunnel)
        connection = ConnectionStore(tunnel: tunnel, servers: servers, settings: settings)
        restartClock()
    }

    deinit {
        defaults.removePersistentDomain(forName: suite)
    }

    func load() async {
        await session.restore()
        await servers.refresh()
        await settings.load()
    }

    // Re-anchors the session clock, so every look of a connected screen shows the same time.
    func restartClock() {
        guard let connectedFor else { return }
        tunnel.connectedDate = Date().addingTimeInterval(-connectedFor)
        tunnel.onChange?()
    }

    // Mirrors RootView's container, which every screen sits in.
    func stage<V: View>(_ view: V) -> some View {
        view
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Palette.bg.ignoresSafeArea())
            .environment(session)
            .environment(servers)
            .environment(settings)
            .environment(connection)
    }

    // A fixed path opens already pushed, with no transition to wait out; only for screens that
    // take their destinations from outside, since a stack nested inside another one misbehaves.
    func pushed<Root: View, Route: Hashable, Destination: View>(
        _ root: Root,
        path: [Route],
        @ViewBuilder destination: @escaping (Route) -> Destination
    ) -> some View {
        stage(
            NavigationStack(path: .constant(path)) {
                root.navigationDestination(for: Route.self, destination: destination)
            }
        )
    }
}
