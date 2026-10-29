import SwiftUI

struct MainView: View {
    @Environment(SessionStore.self) private var session
    @Environment(ConnectionStore.self) private var connection
    @Environment(SettingsStore.self) private var settings
    @Environment(\.colorScheme) private var colorScheme
    @State private var showSettings = false

    private static let maxMapSize: CGFloat = 236
    private static let gutter: CGFloat = 20

    init() {}

    private var serverSheetShown: Binding<Bool> {
        Binding(get: { connection.showServerSheet }, set: { connection.showServerSheet = $0 })
    }

    var body: some View {
        VStack(spacing: 0) {
            PangeaHeader(
                version: "v\(Self.versionName)",
                onToggleTheme: toggleTheme,
                onMenu: { showSettings = true }
            )
            .sheet(isPresented: $showSettings) { SettingsSheet() }
            GeometryReader { proxy in
                ScrollView {
                    content(viewport: proxy.size)
                }
                .scrollBounceBehavior(.basedOnSize)
            }
        }
        .background(Palette.bg.ignoresSafeArea())
        .sheet(isPresented: serverSheetShown) { ServerSheet() }
    }

    private func content(viewport: CGSize) -> some View {
        let status = connection.status
        return VStack(alignment: .leading, spacing: 0) {
            map(state: status.state, viewport: viewport)
            if SubscriptionBanner.shouldShow(session.subscription) {
                SubscriptionBanner(subscription: session.subscription)
                    .padding(.bottom, 16)
            }
            HeroKicker(state: status.state)
            HeroHeadline(state: status.state)
                .padding(.top, 6)
            HeroDetail(detail(for: status))
                .padding(.top, 6)
            facts(status)
            SectionLabel(String(localized: "hero_region", defaultValue: "Region"))
                .padding(.top, 20)
                .padding(.bottom, 9)
            RegionSlots()
            ConnectButton(state: status.state, action: toggleConnection)
                .padding(.top, 18)
        }
        .padding(.horizontal, Self.gutter)
        .padding(.bottom, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // Height caps it too, so a short or landscape screen keeps the connect button in reach.
    private func map(state: TunnelState, viewport: CGSize) -> some View {
        let available = viewport.width - Self.gutter * 2
        let size = max(0, min(available, viewport.height * 0.38, Self.maxMapSize))
        return DriftMap(state: state)
            .frame(width: size, height: size)
            .frame(maxWidth: .infinity)
    }

    private func facts(_ status: TunnelStatus) -> some View {
        let connected = status.state == .connected
        let started = connection.connectedDate
        let via = TransportLabels.status(status.activeTransport)
        return TimelineView(.periodic(from: .now, by: 1)) { context in
            FactsStrip(
                session: Format.sessionClock(state: status.state, connectedDate: started, now: context.date),
                down: connected ? Format.bytes(status.bytesIn) : Format.emDash,
                up: connected ? Format.bytes(status.bytesOut) : Format.emDash,
                via: via.isEmpty ? Format.emDash : via
            )
        }
    }

    // A connect that throws never changes the tunnel state, so its error borrows the detail line.
    private func detail(for status: TunnelStatus) -> String {
        if status.state == .disconnected || status.state == .error, let error = connection.error {
            return error
        }
        return status.detail.isEmpty ? status.serverName : status.detail
    }

    private func toggleConnection() {
        Task { await connection.toggle() }
    }

    private func toggleTheme() {
        settings.setDarkTheme(colorScheme != .dark)
    }

    private static var versionName: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
    }
}

#if DEBUG
private let mainPreviewServers: [Server] = [
    Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 22),
    Server(id: "uk-lon-2", name: "London", region: "eu", country: "GB", load: 61),
    Server(id: "de-fra-1", name: "Frankfurt", region: "eu", country: "DE", load: 48),
    Server(id: "us-nyc-1", name: "New York", region: "us", country: "US", load: 73),
    Server(id: "sg-1", name: "Singapore", region: "ap", country: "SG", load: 35),
]

private let activeSubscription = Subscription(status: "active", renews: true, expiresAt: "2026-12-01")

private struct MainPreviewHost: View {
    let session: SessionStore
    let servers: ServersStore
    let connection: ConnectionStore
    let settings: SettingsStore

    var body: some View {
        MainView()
            .environment(session)
            .environment(servers)
            .environment(connection)
            .environment(settings)
            .task {
                await servers.refresh()
                await session.loadSubscription()
            }
    }
}

private func previewStatus(_ state: TunnelState, detail: String = "", bytesIn: Int64 = 0,
                           bytesOut: Int64 = 0, transport: String? = nil) -> TunnelStatus {
    TunnelStatus(
        state: state, detail: detail, bytesIn: bytesIn, bytesOut: bytesOut,
        serverId: "us-nyc-1", serverName: "New York", activeTransport: transport)
}

@MainActor
private func mainPreview(_ status: TunnelStatus, connectedFor seconds: TimeInterval? = nil,
                         subscription: Subscription? = nil) -> MainPreviewHost {
    let core = FakeCore()
    var config = AppSettings()
    config.lastServerId = status.serverId
    core.configResult = .success(config)
    core.serversResult = .success(mainPreviewServers)
    core.subscriptionResult = .success(subscription)
    let tunnel = FakeTunnel()
    tunnel.connectedDate = seconds.map { Date().addingTimeInterval(-$0) }
    tunnel.status = status
    let settings = SettingsStore(core: core)
    let servers = ServersStore(core: core)
    return MainPreviewHost(
        session: SessionStore(core: core, tunnel: tunnel),
        servers: servers,
        connection: ConnectionStore(tunnel: tunnel, servers: servers, settings: settings),
        settings: settings)
}

#Preview("Disconnected") {
    mainPreview(.disconnected)
}

#Preview("Connecting") {
    mainPreview(previewStatus(.connecting), subscription: activeSubscription)
}

#Preview("Connected") {
    mainPreview(
        previewStatus(.connected, bytesIn: 1_572_864, bytesOut: 327_680, transport: "reality"),
        connectedFor: 754,
        subscription: activeSubscription)
}

#Preview("Error") {
    mainPreview(
        previewStatus(.error, detail: "The server did not answer the handshake."),
        subscription: Subscription(status: "past_due", renews: false, expiresAt: "2026-10-01"))
}
#endif
