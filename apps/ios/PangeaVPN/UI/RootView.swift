import SwiftUI

struct RootView: View {
    @Environment(SessionStore.self) private var session
    @Environment(ServersStore.self) private var servers
    @Environment(SettingsStore.self) private var settings
    @Environment(\.scenePhase) private var scenePhase
    @State private var hasRestored = false
    @State private var draftToken = ""

    init() {}

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Palette.bg.ignoresSafeArea())
            .overlay {
                if scenePhase != .active { PrivacyCover() }
            }
            .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
            .onChange(of: session.restoring) { was, now in
                if was && !now { hasRestored = true }
            }
            .onChange(of: session.session != nil) { _, signedIn in
                if signedIn { draftToken = "" }
            }
            .task {
                await session.restore()
                hasRestored = true
            }
            .task(id: hasRestored && session.session != nil) { await loadSignedInData() }
    }

    @ViewBuilder
    private var content: some View {
        if !hasRestored {
            SplashView()
        } else if session.showDeviceLimit {
            DeviceLimitView()
        } else if session.session == nil {
            LoginView(token: $draftToken)
        } else {
            MainView()
        }
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
}

struct SplashView: View {
    var body: some View {
        BrandLogo(size: 72)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Palette.bg.ignoresSafeArea())
            .accessibilityHidden(true)
    }
}

private struct PrivacyCover: View {
    var body: some View {
        SplashView()
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
    return RootView()
        .environment(SessionStore(core: core, tunnel: tunnel))
        .environment(servers)
        .environment(settings)
        .environment(ConnectionStore(tunnel: tunnel, servers: servers, settings: settings))
}
#endif
