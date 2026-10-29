import SwiftUI

struct RootView: View {
    @Environment(SessionStore.self) private var session
    @Environment(ServersStore.self) private var servers
    @Environment(SettingsStore.self) private var settings
    @State private var hasRestored = false

    init() {}

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Palette.bg.ignoresSafeArea())
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
            LoginView()
        } else {
            MainView()
        }
    }

    private func loadSignedInData() async {
        guard hasRestored, session.session != nil else { return }
        async let loadedSettings: Void = settings.load()
        async let refreshedServers: Void = servers.refresh()
        _ = await (loadedSettings, refreshedServers)
    }
}

private struct SplashView: View {
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
    return RootView()
        .environment(SessionStore(core: core, tunnel: tunnel))
        .environment(servers)
        .environment(settings)
        .environment(ConnectionStore(tunnel: tunnel, servers: servers, settings: settings))
}
#endif
