import SwiftUI

struct RootView: View {
    @Environment(SessionStore.self) private var session
    @Environment(ServersStore.self) private var servers
    @Environment(SettingsStore.self) private var settings
    @Environment(ConnectionStore.self) private var connection
    @Environment(PurchaseStore.self) private var purchases
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.scenePhase) private var scenePhase
    @State private var hasRestored = false
    @State private var draftToken = ""
    @State private var lastSubscriptionCheck: Date?
    @State private var wipe: Task<Void, Never>?
    @State private var noticeAccepted = DataNotice.isAccepted(in: .standard)

    init() {}

    private var erasing: Bool { wipe != nil || PanicTrigger.shared.requested }

    private enum Screen { case splash, dataNotice, deviceLimit, restoreFailed, login, main }

    private var screen: Screen {
        if !hasRestored || erasing { return .splash }
        if !noticeAccepted { return .dataNotice }
        if session.showDeviceLimit { return .deviceLimit }
        if session.session == nil { return session.restoreFailed ? .restoreFailed : .login }
        return .main
    }

    var body: some View {
        content
            .animation(Motion.screen, value: screen)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Palette.bg.ignoresSafeArea())
            .privacyCover()
            .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
            .onChange(of: session.restoring) { was, now in
                if was && !now { hasRestored = true }
            }
            .onChange(of: session.session != nil) { _, signedIn in
                // A picker left open by a sign-out or wipe would reopen over the next account.
                guard signedIn else {
                    connection.showServerSheet = false
                    return
                }
                draftToken = ""
                lastSubscriptionCheck = Date()
            }
            .onChange(of: scenePhase) { _, phase in
                sceneChanged(to: phase)
            }
            .onChange(of: PanicTrigger.shared.requested, initial: true) { _, requested in
                guard requested else { return }
                Task { await panicWipe() }
            }
            .onChange(of: purchases.notice) { _, notice in
                guard let notice else { return }
                toasts.show(notice.text, style: notice.style)
            }
            .onChange(of: purchases.sheet) { _, sheet in
                // A sign-in that failed after buying leaves the new number ready to retry.
                guard case .accountNumber(let number) = sheet, session.session == nil else { return }
                draftToken = number
            }
            .sheet(item: purchaseSheet) { sheet in
                purchaseSheetContent(sheet)
            }
            .task { await launch() }
            .task(id: hasRestored && session.session != nil) { await loadSignedInData() }
    }

    // Above every screen, so a purchase that signs in keeps its sheet as sign-in gives way to the main screen.
    private var purchaseSheet: Binding<PurchaseSheet?> {
        Binding(get: { purchases.sheet }, set: { purchases.sheet = $0 })
    }

    @ViewBuilder
    private func purchaseSheetContent(_ sheet: PurchaseSheet) -> some View {
        switch sheet {
        case .paywall:
            PaywallView(onClose: { purchases.closePaywall() })
                .formSheetSized()
        case .accountNumber(let number):
            AccountNumberSheet(number: number, onDone: { purchases.sheet = nil })
                .formSheetSized()
        }
    }

    // Each screen cross-fades into the next rather than cutting.
    @ViewBuilder
    private var content: some View {
        switch screen {
        case .splash: SplashView().transition(.opacity)
        case .dataNotice: DataNoticeView(mode: .firstLaunch, onDone: acceptNotice).transition(.opacity)
        case .deviceLimit: DeviceLimitView().transition(.opacity)
        case .restoreFailed: RestoreFailedView().transition(.opacity)
        case .login: LoginView(token: $draftToken).transition(.opacity)
        case .main: MainView().transition(.opacity)
        }
    }

    // A cold launch from the quick action wipes before restoring, so no signed-in screen shows first.
    private func launch() async {
        RootScenePolicy.record(foreground: true, in: AppGroup.defaults)
        if PanicTrigger.shared.requested { await panicWipe() }
        await session.restore()
        hasRestored = true
        purchases.start()
    }

    private func acceptNotice() {
        DataNotice.accept(in: .standard)
        noticeAccepted = true
    }

    private func loadSignedInData() async {
        guard hasRestored, session.session != nil else {
            if hasRestored { servers.reset() }
            return
        }
        let settings = self.settings
        let servers = self.servers
        async let loadedSettings: Void = settings.load()
        async let refreshedServers: Void = servers.refresh()
        _ = await (loadedSettings, refreshedServers)
    }

    private func sceneChanged(to phase: ScenePhase) {
        RootScenePolicy.record(foreground: RootScenePolicy.isForeground(phase), in: AppGroup.defaults)
        guard phase == .active else { return }
        Task { await becameActive() }
    }

    // The profile waits for loaded settings, so a default Allow LAN never overwrites the saved one.
    private func becameActive() async {
        guard hasRestored, session.session != nil, !erasing else { return }
        if settings.loaded { await connection.applyProfile() }
        let now = Date()
        guard RootScenePolicy.subscriptionDue(lastCheck: lastSubscriptionCheck, now: now, entitled: session.entitled)
        else { return }
        lastSubscriptionCheck = now
        await session.refreshSubscription()
    }

    // Every caller awaits the one wipe in flight, so launch never restores beside it.
    private func panicWipe() async {
        if let running = wipe {
            await running.value
            return
        }
        let running = Task { await erase() }
        wipe = running
        await running.value
        wipe = nil
    }

    private func erase() async {
        purchases.reset()
        await session.wipe()
        settings.resetToDefaults()
        servers.reset()
        PanicShortcut.update(enabled: false)
        PanicTrigger.shared.requested = false
        draftToken = ""
        lastSubscriptionCheck = nil
        // The wipe cleared the app group, foreground flag included.
        RootScenePolicy.record(foreground: RootScenePolicy.isForeground(scenePhase), in: AppGroup.defaults)
        toasts.show(String(localized: "root_data_erased", defaultValue: "PangeaVPN data erased."), style: .success)
    }
}

enum RootScenePolicy {
    static let subscriptionInterval: TimeInterval = 10 * 60
    // While expired the user is likely topping up elsewhere, so coming back checks sooner.
    static let expiredInterval: TimeInterval = 30

    // Inactive still counts: the VPN permission prompt leaves the app on screen.
    static func isForeground(_ phase: ScenePhase) -> Bool {
        switch phase {
        case .active, .inactive: return true
        case .background: return false
        @unknown default: return false
        }
    }

    static func record(foreground: Bool, in defaults: UserDefaults) {
        defaults.set(foreground, forKey: SharedPrefs.Key.appInForeground)
    }

    static func subscriptionDue(lastCheck: Date?, now: Date, entitled: Bool) -> Bool {
        guard let lastCheck else { return true }
        let interval = entitled ? subscriptionInterval : expiredInterval
        return now.timeIntervalSince(lastCheck) >= interval
    }
}

struct SplashView: View {
    var body: some View {
        BrandLogo(size: 72)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Palette.bg.ignoresSafeArea())
            .accessibilityHidden(true)
    }
}

#if DEBUG
#Preview("Splash") {
    SplashView()
}

#Preview("Root") {
    let core = FakeCore()
    let tunnel = FakeTunnel()
    let settings = SettingsStore(core: core)
    let servers = ServersStore(core: core)
    let session = SessionStore(core: core, tunnel: tunnel)
    let toasts = ToastCenter()
    return RootView()
        .environment(session)
        .environment(PurchaseStore(core: core, session: session, store: FakeStoreClient()))
        .environment(servers)
        .environment(settings)
        .environment(ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session))
        .environment(DiagnosticsStore(core: core, tunnel: tunnel))
        .toastOverlay()
        .environment(toasts)
}
#endif
