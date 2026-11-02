import SwiftUI

struct MainView: View {
    @Environment(SessionStore.self) private var session
    @Environment(ServersStore.self) private var servers
    @Environment(ConnectionStore.self) private var connection
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.horizontalSizeClass) private var sizeClass
    @State private var showSettings = false
    @State private var settingsPath: [SettingsRoute] = []
    // Whether this tunnel has been up since it last stopped, so a reassert reads as a reconnect.
    @State private var sawConnected = false
    @State private var bannerHeight: CGFloat = 0
    @State private var heroHeight: CGFloat = 0
    @State private var slotMetrics = SlotMetrics()
    @State private var stageWidth: CGFloat = 0
    // Unset until the width is known, then held while the picker is up so a resize never swaps its presentation.
    @State private var pickerOnSheet: Bool?
    @State private var wideBannerHeight: CGFloat = 0
    @State private var wideHeroHeight: CGFloat = 0
    @State private var wideFactsHeight: CGFloat = 0
    @State private var wideSlotMetrics = SlotMetrics()
    // Tall enough for eight idle rows at the default text size, and growing with it.
    @ScaledMetric(relativeTo: .body) private var wideColumnCap: CGFloat = 820

    private static let gutter: CGFloat = 20
    private static let topInset: CGFloat = 2
    private static let bottomInset: CGFloat = 12
    private static let slotsTop: CGFloat = 16
    private static let factsTop: CGFloat = 14
    private static let wideGutter: CGFloat = 28
    private static let wideHeroWidth: CGFloat = 560
    private static let wideStageInset: CGFloat = 16
    private static let wideBannerGap: CGFloat = 14
    private static let expiredWidth: CGFloat = 400

    init() {}

    private var isWide: Bool { AppLayout.isWide(width: stageWidth, sizeClass: sizeClass) }

    private func regionPickerShown(onSheet: Bool) -> Binding<Bool> {
        Binding(get: { connection.showServerSheet && pickerOnSheet == onSheet },
                set: { connection.showServerSheet = $0 })
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

    private var action: MainAction {
        MainAction.resolve(state: status.state, busy: connection.busy, switching: connection.switching,
                           reconnecting: reconnecting, lockdownBlocks: connection.lockdownBlocksDisconnect,
                           entitled: connection.entitled)
    }

    var body: some View {
        VStack(spacing: 0) {
            PangeaHeader(version: "v\(Self.versionName)", onToggleTheme: toggleTheme, onMenu: { openSettings() })
            AdaptiveLayout(wide: { wideStage }, compact: { compactStage })
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { measured(width: $0) }
        }
        // Capped here only, so the screens it opens keep the accessibility sizes.
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
        .background(Palette.bg.ignoresSafeArea())
        .fullScreenCover(isPresented: $showSettings) { SettingsView(path: settingsPath) }
        .fullScreenCover(isPresented: regionPickerShown(onSheet: false)) { RegionPickerView() }
        .sheet(isPresented: regionPickerShown(onSheet: true)) { RegionPickerView().formSheetSized() }
        .onAppear { sawConnected = status.state == .connected }
        .onChange(of: status.state) { _, state in track(state) }
        .onChange(of: connection.error) { _, message in toast(message) }
        .onChange(of: isWide) { _, wide in
            if !connection.showServerSheet { pickerOnSheet = wide }
        }
        .onChange(of: connection.showServerSheet) { _, shown in
            if !shown { pickerOnSheet = isWide }
        }
    }

    // A picker already asked for before the first layout waits for this, so it opens in the right style.
    private func measured(width: CGFloat) {
        stageWidth = width
        if pickerOnSheet == nil, width > 0 {
            pickerOnSheet = AppLayout.isWide(width: width, sizeClass: sizeClass)
        }
    }

    private var compactStage: some View {
        VStack(spacing: 0) {
            GeometryReader { proxy in
                ScrollView {
                    content(width: proxy.size.width - Self.gutter * 2, viewport: proxy.size.height)
                }
                .scrollBounceBehavior(.basedOnSize)
            }
            actionBar(inset: Self.gutter)
        }
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
                    .transition(Motion.rise(reduceMotion, by: -8))
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { bannerHeight = $0 }
            }
            if connection.entitled {
                hero
                    .padding(.top, 10)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { heroHeight = $0 }
                if showsFacts {
                    facts(width: width)
                        .padding(.top, Self.factsTop)
                        .transition(factsTransition)
                        // Beneath the regions, which slide off it as it fades in rather than overprint it.
                        .zIndex(-1)
                }
                RegionSlots(rows: rows, metrics: $slotMetrics)
                    .padding(.top, Self.slotsTop)
            } else {
                ExpiredPanel()
                    .padding(.top, 8)
            }
        }
        .animation(Motion.layout(reduceMotion), value: showsFacts)
        .animation(Motion.layout(reduceMotion), value: rows)
        .animation(Motion.layout(reduceMotion), value: banner)
        .padding(.horizontal, Self.gutter)
        .padding(.top, Self.topInset)
        .padding(.bottom, Self.bottomInset)
    }

    private func slotRows(viewport: CGFloat, banner: Bool) -> Int {
        guard !showsFacts else { return Regions.slotCount }
        let fixed = (banner ? bannerHeight : 0) + heroHeight + Self.slotsTop
        return SlotBudget.idleRows(viewport: viewport, fixed: fixed, metrics: slotMetrics)
    }

    // Desktop's stage: the map bleeding off the left edge, the hero column beside it.
    private var wideStage: some View {
        GeometryReader { proxy in
            HStack(spacing: 0) {
                wideMap(column: MapSizing.wideColumn(width: proxy.size.width), height: proxy.size.height)
                wideColumn(height: proxy.size.height)
            }
        }
    }

    private func wideMap(column: CGFloat, height: CGFloat) -> some View {
        let side = MapSizing.wideSide(column: column, height: height - Self.wideStageInset * 2)
        return DriftMap(state: display)
            .frame(width: side, height: side)
            .padding(.leading, -side * MapSizing.wideBleed)
            .frame(width: column, height: height, alignment: .leading)
    }

    // A fixed-height column centred beside the map, so state changes never move the hero or the button.
    private func wideColumn(height: CGFloat) -> some View {
        let columnHeight = max(min(height - Self.wideStageInset * 2, wideColumnCap), 0)
        return VStack(spacing: 0) {
            GeometryReader { proxy in
                ScrollView {
                    wideContent(width: proxy.size.width - Self.wideGutter * 2, viewport: proxy.size.height)
                }
                .scrollBounceBehavior(.basedOnSize)
            }
            actionBar(inset: Self.wideGutter)
        }
        .frame(maxWidth: Self.wideHeroWidth + Self.wideGutter * 2)
        .frame(height: columnHeight)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func wideContent(width: CGFloat, viewport: CGFloat) -> some View {
        let height = max(viewport - Self.topInset - Self.bottomInset, 0).rounded(.down)
        let banner = SubscriptionBanner.shouldShow(session.subscription)
        let rows = wideRows(viewport: height, banner: banner)
        return VStack(alignment: .leading, spacing: 0) {
            if banner {
                SubscriptionBanner(subscription: session.subscription)
                    .padding(.bottom, Self.wideBannerGap)
                    .transition(Motion.rise(reduceMotion, by: -8))
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { wideBannerHeight = $0 }
            }
            if connection.entitled {
                hero
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { wideHeroHeight = $0 }
                if showsFacts {
                    facts(width: width)
                        .padding(.top, Self.factsTop)
                        .transition(factsTransition)
                        .zIndex(-1)
                }
                RegionSlots(rows: rows, metrics: $wideSlotMetrics)
                    .padding(.top, Self.slotsTop)
            } else {
                ExpiredPanel()
                    .frame(maxWidth: Self.expiredWidth)
                    .frame(maxWidth: .infinity, minHeight: max(height - (banner ? wideBannerHeight : 0), 0))
            }
        }
        .background(alignment: .top) { factsProbe(width: width) }
        .animation(Motion.layout(reduceMotion), value: showsFacts)
        .animation(Motion.layout(reduceMotion), value: rows)
        .animation(Motion.layout(reduceMotion), value: banner)
        .padding(.horizontal, Self.wideGutter)
        .padding(.top, Self.topInset)
        .padding(.bottom, Self.bottomInset)
    }

    private func wideRows(viewport: CGFloat, banner: Bool) -> Int {
        let facts = showsFacts ? Self.factsTop + wideFactsHeight : 0
        let fixed = (banner ? wideBannerHeight : 0) + wideHeroHeight + facts + Self.slotsTop
        return SlotBudget.wideRows(viewport: viewport, fixed: fixed, metrics: wideSlotMetrics)
    }

    // Measured before the facts first show, so connecting trims the list in one move rather than two.
    private func factsProbe(width: CGFloat) -> some View {
        let dash = Format.emDash
        return FactsStrip(session: dash, down: dash, up: dash, via: dash, width: width)
            .fixedSize(horizontal: false, vertical: true)
            .hidden()
            .accessibilityHidden(true)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { wideFactsHeight = $0 }
    }

    // The facts leave quickly, so the regions sliding up never pass over a half-faded grid.
    private var factsTransition: AnyTransition {
        if reduceMotion { return .opacity }
        return .asymmetric(insertion: Motion.rise(false, by: -10), removal: .opacity.animation(Motion.quick))
    }

    private var hero: some View {
        let shown = display
        return VStack(alignment: .leading, spacing: 6) {
            HeroKicker(
                state: shown,
                postQuantum: HeroChips.showsPostQuantum(display: shown, postQuantum: status.postQuantum),
                split: HeroChips.showsSplit(display: shown, enabled: settings.splitTunnelEnabled,
                                            ranges: settings.splitTunnelRanges, strictKillSwitch: settings.killSwitch))
            FadeSwap(shown) { HeroHeadline(state: $0) }
            FadeSwap(HeroDetailText.resolve(
                display: shown, statusDetail: status.detail, serverName: status.serverName,
                error: connection.error, progress: connection.progress, paused: reconnecting)) { HeroDetail($0) }
            if !status.entryServerId.isEmpty {
                RoutePath(
                    entry: RouteNode.resolve(id: status.entryServerId, fallbackName: status.entryServerName,
                                             in: servers.regions),
                    exit: RouteNode.resolve(id: status.serverId, fallbackName: status.serverName, in: servers.regions),
                    live: shown == .connected)
                    .padding(.top, 4)
                    .transition(.opacity)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        // Colour and wording cross-fade on every state change, not only the idle ones.
        .animation(Motion.state(reduceMotion), value: shown)
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
    private func actionBar(inset: CGFloat) -> some View {
        let kind = action
        if kind != .hidden {
            Group {
                if kind == .lockdown {
                    LockdownNote { openSettings(at: [.startup]) }
                } else {
                    ConnectButton(kind: kind, action: toggleConnection)
                }
            }
            .padding(.horizontal, inset)
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

#Preview("Wide, disconnected", traits: .fixedLayout(width: 1180, height: 820)) {
    MainPreview(stores: MainPreviewStores())
        .environment(\.horizontalSizeClass, .regular)
}

#Preview("Wide, multihop", traits: .fixedLayout(width: 1180, height: 820)) {
    MainPreview(stores: MainPreviewStores(
        status: MainPreviewData.status(.connected, serverId: "us-nyc-1", serverName: "New York",
                                       bytesIn: 88_431_002, bytesOut: 4_120_000, transport: "hysteria2",
                                       postQuantum: true, entryId: "nl-ams-1", entryName: "Amsterdam"),
        connectedFor: 3_725))
        .environment(\.horizontalSizeClass, .regular)
}

#Preview("Wide portrait, expired", traits: .fixedLayout(width: 834, height: 1194)) {
    MainPreview(stores: MainPreviewStores(subscription: MainPreviewData.expired))
        .environment(\.horizontalSizeClass, .regular)
        .preferredColorScheme(.dark)
}
#endif
