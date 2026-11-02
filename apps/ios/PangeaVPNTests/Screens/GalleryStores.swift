import SwiftUI
@testable import PangeaVPN

enum GalleryData {
    private static let everyTransport: Set<String> = [
        "cloak", "wireguard", "reality", "hysteria2", "shadowsocks", "anytls",
    ]

    // The live regions, so every screenshot shows what the service really offers.
    static let servers: [Server] = [
        Server(id: "eu-london-1", name: "London", region: "London", country: "GB", load: 22, transports: everyTransport, multihop: true),
        Server(id: "eu-london-2", name: "London", region: "London", country: "GB", load: 61, transports: everyTransport, multihop: true),
        Server(id: "eu-central-2", name: "Amsterdam", region: "Amsterdam", country: "NL", load: 15, transports: everyTransport, multihop: true),
        Server(id: "eu-spain-1", name: "Madrid", region: "Madrid", country: "ES", load: 41, transports: everyTransport, multihop: true),
        Server(id: "us-east-1", name: "New York", region: "New York", country: "US", load: 73, transports: everyTransport, multihop: true),
        Server(id: "as-singapore-1", name: "Singapore", region: "Singapore", country: "SG", load: 35, transports: everyTransport),
    ]
    static let session = Session(email: "demo@example.org", name: "Demo", servers: [])
    static let active = Subscription(status: "active", renews: true, expiresAt: "2026-12-01T00:00:00Z", entitled: true)
    static let pastDue = Subscription(status: "past_due", renews: false, expiresAt: "2026-10-01T00:00:00Z", entitled: true)
    static let expired = Subscription(status: "expired", renews: false, expiresAt: "2026-10-20T00:00:00Z", entitled: false)
    static let devices = [
        Device(id: "c0ffee00-1234", friendlyName: "Swift Fox", createdAt: "2026-10-02T19:45:00Z", status: "active", current: true),
        Device(id: "9f2c1a7e-44b0", friendlyName: "Work laptop", createdAt: "2026-09-01T10:00:00Z", status: "active"),
        Device(id: "b81d03c5-7a21", friendlyName: nil, createdAt: "2026-09-12T08:30:00Z", status: "active"),
    ]

    static func status(_ state: TunnelState, detail: String = "", bytesIn: Int64 = 0,
                       bytesOut: Int64 = 0, transport: String? = nil, postQuantum: Bool = false,
                       entry: (id: String, name: String)? = nil) -> TunnelStatus {
        TunnelStatus(state: state, detail: detail, bytesIn: bytesIn, bytesOut: bytesOut,
                     serverId: "us-east-1", serverName: "New York", activeTransport: transport,
                     lastHandshake: Int64(Date().timeIntervalSince1970) - 30, postQuantum: postQuantum,
                     entryServerId: entry?.id ?? "", entryServerName: entry?.name ?? "")
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
    let diagnostics: DiagnosticsStore
    let storeClient: FakeStoreClient
    let purchases: PurchaseStore
    let toasts = ToastCenter()
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
        // RootView reads the notice from standard defaults; the notice has its own shot.
        DataNotice.accept(in: .standard)
        self.connectedFor = connectedFor
        let core = FakeCore()
        var config = AppSettings()
        config.lastServerId = status.serverId.isEmpty ? (list.first?.id ?? "") : status.serverId
        core.configResult = .success(config)
        core.serversResult = .success(list)
        core.subscriptionResult = .success(subscription)
        core.restoreResult = .success(GalleryData.session)
        core.devicesResult = .success(GalleryData.devices)
        core.accountNumberValue = "ABCDEFGHJKMNPQRSTVWXYZ01"
        core.hubStatusValue = HubStatus(active: "reality", detail: nil)
        core.logsValue = "12:00:01 info tunnel started\n12:00:02 info reality transport carried the first handshake"
        let tunnel = FakeTunnel()
        tunnel.status = status
        tunnel.logsValue = "12:00:01 info extension started"
        let servers = ServersStore(core: core, defaults: test.defaults)
        let settings = SettingsStore(core: core, defaults: test.defaults)
        let session = SessionStore(core: core, tunnel: tunnel)
        self.core = core
        self.tunnel = tunnel
        self.servers = servers
        self.settings = settings
        self.session = session
        connection = ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session)
        diagnostics = DiagnosticsStore(core: core, tunnel: tunnel)
        let storeClient = FakeStoreClient()
        self.storeClient = storeClient
        purchases = PurchaseStore(core: core, session: session, store: storeClient)
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
            .environment(diagnostics)
            .environment(purchases)
            .toastOverlay()
            .environment(toasts)
            // A bare hosting controller has no active scene, so the privacy cover would hide the screen.
            .environment(\.scenePhase, .active)
    }

    static let narrowWidth: CGFloat = 375

    // An iPhone-wide window at the left of the screen, as in Split View, so the compact fallback shows.
    func stageNarrow<V: View>(_ view: V) -> some View {
        HStack(spacing: 0) {
            stage(view)
                .frame(width: Self.narrowWidth)
                .environment(\.horizontalSizeClass, .compact)
            Color.black.ignoresSafeArea()
        }
    }
}
