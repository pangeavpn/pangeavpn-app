import SwiftUI

// Desktop's settings rail as a phone home screen: every rail item pushes its own page.
struct SettingsView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session
    @Environment(ToastCenter.self) private var toasts
    @State private var path: [SettingsRoute]
    @State private var confirmingSignOut = false
    @State private var versionTaps = VersionTapUnlock()

    init(path: [SettingsRoute] = []) {
        _path = State(initialValue: path)
    }

    var body: some View {
        NavigationStack(path: $path) {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    header
                    AccountCard()
                        .padding(.top, 8)
                    routeRows
                        .padding(.top, 20)
                    accountActions
                        .padding(.top, 20)
                    versionLine
                        .padding(.top, 12)
                }
                .padding(.horizontal, 16)
                .padding(.bottom, 20)
            }
            .background(Palette.bg.ignoresSafeArea())
            .navigationTitle(Self.title)
            .toolbar(.hidden, for: .navigationBar)
            .navigationDestination(for: SettingsRoute.self) { route in
                destination(route)
            }
        }
        .tint(Palette.accent)
        .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
        // A full-screen cover sits above the app-wide toast layer, so this screen carries its own.
        .toastOverlay()
        .task {
            if !settings.loaded { await settings.load() }
        }
    }

    private static var title: String {
        String(localized: "settings_title", defaultValue: "Settings")
    }

    private var header: some View {
        HStack(spacing: 8) {
            Text(Self.title)
                .scaledFont(size: 28, weight: .semibold, relativeTo: .largeTitle)
                .tracking(-0.4)
                .foregroundStyle(Palette.text)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 8)
            SettingsCloseButton { dismiss() }
                .background(Palette.surface, in: Circle())
                .overlay(Circle().strokeBorder(Palette.borderSubtle, lineWidth: 1))
        }
        .padding(.top, 8)
    }

    private var routeRows: some View {
        VStack(spacing: 8) {
            ForEach(homeRoutes, id: \.self) { route in
                NavigationLink(value: route) {
                    SettingsNavRow(systemImage: route.systemImage, title: route.title, value: summary(route))
                }
                .buttonStyle(.plain)
            }
        }
    }

    private var homeRoutes: [SettingsRoute] {
        SettingsRoute.home.filter { $0 != .developer || settings.developerMode }
    }

    private func summary(_ route: SettingsRoute) -> String? {
        switch route {
        case .transport: return SettingsSummary.transport(settings.settings)
        case .provisioning: return SettingsSummary.provisioning(settings.settings)
        case .network: return SettingsSummary.network(settings.settings)
        case .killSwitch: return SettingsSummary.killSwitch(settings)
        case .splitTunnel: return SettingsSummary.splitTunnel(settings)
        case .startup: return SettingsSummary.startup(settings)
        case .notifications: return SettingsSummary.notifications(settings)
        case .panic: return SettingsSummary.panic(settings)
        case .language: return SettingsSummary.language()
        case .appearance: return SettingsSummary.appearance(settings)
        case .update: return SettingsSummary.update()
        case .developer: return SettingsSummary.developer(settings)
        case .support, .devices: return nil
        }
    }

    @ViewBuilder
    private func destination(_ route: SettingsRoute) -> some View {
        switch route {
        case .transport: TransportPage()
        case .provisioning: ProvisioningPage()
        case .network: NetworkPage()
        case .killSwitch: KillSwitchPage()
        case .splitTunnel: SplitTunnelPage()
        case .startup: StartupPage()
        case .notifications: NotificationsPage()
        case .panic: PanicPage()
        case .language: LanguagePage()
        case .appearance: AppearancePage()
        case .support: SupportPage()
        case .update: UpdatePage()
        case .developer: DeveloperPage()
        case .devices: DevicesPage()
        }
    }

    private var accountActions: some View {
        VStack(spacing: 8) {
            NavigationLink(value: SettingsRoute.devices) {
                SettingsNavRow(systemImage: SettingsRoute.devices.systemImage, title: SettingsRoute.devices.title)
            }
            .buttonStyle(.plain)
            Button { confirmingSignOut = true } label: {
                SettingsNavRow(systemImage: "rectangle.portrait.and.arrow.right", title: Self.signOutTitle, tint: Palette.error)
                    .opacity(session.signingOut ? 0.5 : 1)
                    .overlay(alignment: .trailing) {
                        if session.signingOut {
                            ProgressView()
                                .tint(Palette.error)
                                .padding(.trailing, 40)
                        }
                    }
            }
            .buttonStyle(.plain)
            .disabled(session.signingOut)
            .confirmationDialog(
                String(localized: "settings_home_signout_title", defaultValue: "Sign out of PangeaVPN?"),
                isPresented: $confirmingSignOut,
                titleVisibility: .visible
            ) {
                Button(Self.signOutTitle, role: .destructive) { signOut() }
                Button(String(localized: "settings_home_cancel", defaultValue: "Cancel"), role: .cancel) {}
            } message: {
                Text(String(localized: "settings_home_signout_body", defaultValue: "The VPN disconnects and this device leaves your account. You'll need your account number to sign in again."))
            }
        }
    }

    private static var signOutTitle: String {
        String(localized: "settings_signout", defaultValue: "Sign Out")
    }

    private var versionLine: some View {
        Text(Self.versionText)
            .scaledFont(size: 12, design: .monospaced, relativeTo: .caption)
            .foregroundStyle(Palette.textMuted)
            .frame(maxWidth: .infinity, minHeight: 44)
            .contentShape(Rectangle())
            .onTapGesture(perform: tapVersion)
    }

    private static var versionText: String {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "0.0.0"
        let build = info?["CFBundleVersion"] as? String ?? "0"
        return String(format: String(localized: "settings_home_version", defaultValue: "PangeaVPN v%1$@ (%2$@)"), version, build)
    }

    private func tapVersion() {
        let next = versionTaps.tapped(at: Date())
        versionTaps = next
        guard next.fired else { return }
        let enabled = !settings.developerMode
        settings.setDeveloperMode(enabled)
        toasts.show(enabled
            ? String(localized: "settings_home_developer_on", defaultValue: "Developer options on")
            : String(localized: "settings_home_developer_off", defaultValue: "Developer options off"))
    }

    // signOut clears the session, which also swaps the screen under this cover to sign-in.
    private func signOut() {
        Task {
            await session.signOut()
            dismiss()
        }
    }
}

#if DEBUG
@MainActor
private func previewSettings(developer: Bool = false) -> some View {
    let core = FakeCore()
    core.subscriptionResult = .success(Subscription(status: "active", renews: true, expiresAt: "2026-12-26T00:00:00Z", entitled: true))
    core.accountNumberValue = "ABCDEFGHJKMNPQRSTVWXYZ01"
    core.devicesResult = .success([
        Device(id: "c0ffee00-1234", friendlyName: "Swift Fox", createdAt: "2026-06-14T19:45:00Z", status: "active", current: true),
        Device(id: "9f2c1a7e-44b0", friendlyName: "Work laptop", createdAt: "2026-09-01T10:00:00Z", status: "active"),
    ])
    // Its own suite, so a preview never writes the real App Group preferences.
    let defaults = UserDefaults(suiteName: "org.pangeavpn.preview.settings") ?? .standard
    let tunnel = FakeTunnel()
    let settings = SettingsStore(core: core, defaults: defaults)
    settings.setDeveloperMode(developer)
    let servers = ServersStore(core: core, defaults: defaults)
    let session = SessionStore(core: core, tunnel: tunnel)
    return SettingsView()
        .environment(session)
        .environment(servers)
        .environment(settings)
        .environment(ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session))
        .environment(DiagnosticsStore(core: core, tunnel: tunnel))
        .environment(ToastCenter())
}

#Preview("Settings home") {
    previewSettings()
}

#Preview("Developer unlocked, dark") {
    previewSettings(developer: true)
        .preferredColorScheme(.dark)
}
#endif
