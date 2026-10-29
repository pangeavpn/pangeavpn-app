import SwiftUI

@main
struct PangeaVPNApp: App {
    @State private var session: SessionStore
    @State private var servers: ServersStore
    @State private var settings: SettingsStore
    @State private var connection: ConnectionStore

    init() {
        let core = GoCore.shared(sink: nil)
        let tunnel = TunnelController()
        let settings = SettingsStore(core: core)
        let servers = ServersStore(core: core)
        _session = State(initialValue: SessionStore(core: core, tunnel: tunnel))
        _servers = State(initialValue: servers)
        _settings = State(initialValue: settings)
        _connection = State(initialValue: ConnectionStore(tunnel: tunnel, servers: servers, settings: settings))
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(session)
                .environment(servers)
                .environment(settings)
                .environment(connection)
                .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
        }
    }
}
