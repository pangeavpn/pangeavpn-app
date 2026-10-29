import SwiftUI

@main
struct PangeaVPNApp: App {
    @State private var session: SessionStore
    @State private var servers: ServersStore
    @State private var settings: SettingsStore
    @State private var connection: ConnectionStore
    @State private var toasts = ToastCenter()

    init() {
        let core = GoCore.forApp()
        let tunnel = TunnelController()
        let settings = SettingsStore(core: core)
        let servers = ServersStore(core: core)
        let session = SessionStore(core: core, tunnel: tunnel)
        _session = State(initialValue: session)
        _servers = State(initialValue: servers)
        _settings = State(initialValue: settings)
        _connection = State(initialValue: ConnectionStore(
            tunnel: tunnel, servers: servers, settings: settings, session: session))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(session)
                .environment(servers)
                .environment(settings)
                .environment(connection)
                .environment(toasts)
                .toastOverlay()
        }
    }
}
