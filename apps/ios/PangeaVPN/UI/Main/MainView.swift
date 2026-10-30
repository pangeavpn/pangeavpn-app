import SwiftUI

struct MainView: View {
    @Environment(SessionStore.self) private var session
    @Environment(ServersStore.self) private var servers
    @Environment(ConnectionStore.self) private var connection
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var showSettings = false
    @State private var settingsPath: [SettingsRoute] = []
    // Whether this tunnel has been up since it last stopped, so a reassert reads as a reconnect.
    @State private var sawConnected = false
    @State private var bannerHeight: CGFloat = 0
    @State private var heroHeight: CGFloat = 0
    @State private var slotMetrics = SlotMetrics()

    private static let gutter: CGFloat = 20
    private static let topInset: CGFloat = 2
    private static let bottomInset: CGFloat = 12
    private static let slotsTop: CGFloat = 16

    init() {}

    private var regionPickerShown: Binding<Bool> {
        Binding(get: { connection.showServerSheet }, set: { connection.showServerSheet = $0 })
    }

    private var status: TunnelStatus { connection.status }

    private var display: TunnelState {
        HeroState.display(status.state, busy: connection.busy, switching: connection.switching)
    }

    private var reconnecting: Bool {
        HeroState.reconnecting(status.state, busy: connection.busy, sawConnected: sawConnected,
                               lastHandshake: status.lastHandshake)
    }

    // Idle has no session to report, so the facts give their room to more regions.
    private var showsFacts: Bool { display != .disconnected && display != .error }

    private var motion: Animation {
        reduceMotion ? .easeInOut(duration: 0.2) : .spring(response: 0.5, dampingFraction: 0.86)
    }

    private var action: MainAction {
        MainAction.resolve(state: status.state, busy: connection.busy, switching: connection.switching,
                           reconnecting: reconnecting, lockdownBlocks: connection.lockdownBlocksDisconnect,
                           entitled: connection.entitled)
    }

    var body: some View {
        VStack(spacing: 0) {
            PangeaHeader(version: "v\(Self.versionName)", onToggleTheme: toggleTheme, onMenu: { openSettings() })
            GeometryReader { proxy in
                ScrollView {
                    content(width: proxy.size.width - Self.gutter * 2, viewport: proxy.size.height)
                }
                .scrollBounceBehavior(.basedOnSize)
            }
            actionBar
        }
        // Capped here only, so the screens it opens keep the accessibility sizes.
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .background(Palette.bg.ignoresSafeArea())
        .fullScreenCover(isPresented: $showSettings) { SettingsView(path: settingsPath) }
        .fullScreenCover(isPresented: regionPickerShown) { RegionPickerView() }
        .onAppear { sawConnected = status.state == .connected }
        .onChange(of: status.state) { _, state in track(state) }
        .onChange(of: connection.error) { _, message in toast(message) }
    }

    // Rounded down, so the stack never ends a fraction of a point past the screen and scrolls.
    private func content(width: CGFloat, viewport: CGFloat) -> some View {
        let height = max(viewport - Self.topInset - Self.bottomInset, 0).rounded(.down)
        let banner = SubscriptionBanner.shouldShow(session.subscription)
        let rows = slotRows(viewport: height, banner: banner)
        return MainLayout(viewportHeight: height) {
            DriftMap(state: display)
                .layoutValue(key: MapSlot.self, value: true)
            if banner {
                SubscriptionBanner(subscription: session.subscription)
                    .padding(.top, 6)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bannerHeight = $0 }
            }
            if connection.entitled {
                hero
                    .padding(.top, 10)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { heroHeight = $0 }
                if showsFacts {
                    facts(width: width)
                        .padding(.top, 14)
                        .transition(factsTransition)
                }
                RegionSlots(rows: rows, metrics: $slotMetrics)
                    .padding(.top, Self.slotsTop)
            } else {
                ExpiredPanel()
                    .padding(.top, 8)
            }
        }
        .animation(motion, value: showsFacts)
        .animation(motion, value: rows)
        .padding(.horizontal, Self.gutter)
        .padding(.top, Self.topInset)
        .padding(.bottom, Self.bottomInset)
    }

    private func slotRows(viewport: CGFloat, banner: Bool) -> Int {
        guard !showsFacts else { return Regions.slotCount }
        let fixed = (banner ? bannerHeight : 0) + heroHeight + Self.slotsTop
        return SlotBudget.idleRows(viewport: viewport, fixed: fixed, metrics: slotMetrics)
    }

    // The facts leave quickly, so the regions sliding up never pass over a half-faded grid.
    private var factsTransition: AnyTransition {
        if reduceMotion { return .opacity }
        return .asymmetric(insertion: .opacity.combined(with: .offset(y: -10)),
                           removal: .opacity.animation(.easeOut(duration: 0.14)))
    }

    private var hero: some View {
        let shown = display
        return VStack(alignment: .leading, spacing: 6) {
            HeroKicker(
                state: shown,
                postQuantum: HeroChips.showsPostQuantum(display: shown, postQuantum: status.postQuantum),
                split: HeroChips.showsSplit(display: shown, enabled: settings.splitTunnelEnabled,
                                            ranges: settings.splitTunnelRanges, strictKillSwitch: settings.killSwitch))
            HeroHeadline(state: shown)
            HeroDetail(HeroDetailText.resolve(
                display: shown, statusDetail: status.detail, serverName: status.serverName,
                error: connection.error, progress: connection.progress, paused: reconnecting))
            if !status.entryServerId.isEmpty {
                RoutePath(
                    entry: RouteNode.resolve(id: status.entryServerId, fallbackName: status.entryServerName,
                                             in: servers.regions),
                    exit: RouteNode.resolve(id: status.serverId, fallbackName: status.serverName, in: servers.regions),
                    live: shown == .connected)
                    .padding(.top, 4)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // The NE stays up through an in-place switch, so the clock and counters follow it, not the hero.
    private func facts(width: CGFloat) -> some View {
        let current = status
        let up = current.state == .connected
        let started = connection.connectedDate
        let via = TransportLabels.status(current.activeTransport)
        return TimelineView(.periodic(from: .now, by: 1)) { context in
            FactsStrip(
                session: Format.sessionClock(state: current.state, connectedDate: started, now: context.date),
                down: up ? Format.bytes(current.bytesIn) : Format.emDash,
                up: up ? Format.bytes(current.bytesOut) : Format.emDash,
                via: via.isEmpty ? Format.emDash : via,
                width: width)
        }
    }

    @ViewBuilder
    private var actionBar: some View {
        let kind = action
        if kind != .hidden {
            Group {
                if kind == .lockdown {
                    LockdownNote { openSettings(at: [.startup]) }
                } else {
                    ConnectButton(kind: kind, action: toggleConnection)
                }
            }
            .padding(.horizontal, Self.gutter)
            .padding(.top, 10)
            .padding(.bottom, 10)
            .background(Palette.bg)
            .overlay(alignment: .top) {
                LinearGradient(colors: [Palette.bg.opacity(0), Palette.bg], startPoint: .top, endPoint: .bottom)
                    .frame(height: 16)
                    .offset(y: -16)
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
        }
    }

    private func track(_ state: TunnelState) {
        switch state {
        case .connected: sawConnected = true
        case .disconnected, .error: sawConnected = false
        case .connecting, .disconnecting: break
        }
    }

    // The detail line carries an idle failure; one that lands while the tunnel is up needs a toast.
    private func toast(_ message: String?) {
        guard let message, !message.isEmpty, !HeroDetailText.carriesError(display) else { return }
        toasts.show(message, style: .warning)
    }

    private func toggleConnection() {
        Task { await connection.toggle() }
    }

    // The Lockdown switch lives on the Startup page, so the note opens settings already there.
    private func openSettings(at path: [SettingsRoute] = []) {
        settingsPath = path
        showSettings = true
    }

    private func toggleTheme() {
        settings.setDarkTheme(colorScheme != .dark)
    }

    private static var versionName: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
    }
}

#if DEBUG
private struct MainPreview: View {
    let stores: MainPreviewStores

    var body: some View {
        stores.attach(MainView())
    }
}

#Preview("Disconnected") {
    MainPreview(stores: MainPreviewStores())
}

#Preview("Connecting") {
    MainPreview(stores: MainPreviewStores(status: MainPreviewData.status(.connecting)))
}

#Preview("Connected, PQ") {
    MainPreview(stores: MainPreviewStores(
        status: MainPreviewData.status(.connected, bytesIn: 1_572_864, bytesOut: 327_680,
                                       transport: "reality", postQuantum: true),
        connectedFor: 754))
}

#Preview("Multihop") {
    MainPreview(stores: MainPreviewStores(
        status: MainPreviewData.status(.connected, serverId: "us-nyc-1", serverName: "New York",
                                       bytesIn: 88_431_002, bytesOut: 4_120_000, transport: "hysteria2",
                                       postQuantum: true, entryId: "nl-ams-1", entryName: "Amsterdam"),
        connectedFor: 3_725))
}

#Preview("Error, past due") {
    MainPreview(stores: MainPreviewStores(
        status: MainPreviewData.status(.error, detail: "The server did not answer the handshake."),
        subscription: MainPreviewData.pastDue))
}

#Preview("Expired") {
    MainPreview(stores: MainPreviewStores(subscription: MainPreviewData.expired))
}

#Preview("No servers, dark") {
    MainPreview(stores: MainPreviewStores(servers: []))
        .preferredColorScheme(.dark)
}
#endif
