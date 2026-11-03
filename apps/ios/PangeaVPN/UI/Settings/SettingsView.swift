import SwiftUI

// Desktop's settings rail: a phone pushes each item's page, a wide window shows it beside the rail.
struct SettingsView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session
    @Environment(ToastCenter.self) private var toasts
    // Both layouts read it, so a resize across the threshold keeps the open page.
    @State private var path: [SettingsRoute]
    @State private var confirmingSignOut = false
    @State private var versionTaps = VersionTapUnlock()
    private let closable: Bool

    // Fits the account number on one line at the default text size.
    private static let railWidth: CGFloat = 320
    private static let signOutImage = "rectangle.portrait.and.arrow.right"
    private static let deleteImage = "trash"

    // A swipe-down sheet has its grabber instead, so only the full-screen cover shows the close button.
    init(path: [SettingsRoute] = [], closable: Bool = true) {
        _path = State(initialValue: path)
        self.closable = closable
    }

    var body: some View {
        AdaptiveLayout {
            wideBody
        } compact: {
            compactBody
        }
        .tint(Palette.accent)
        .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
        // A cover or sheet sits above the app-wide toast and privacy layers, so it carries its own.
        .toastOverlay()
        .privacyCover()
        .task {
            if !settings.loaded { await settings.load() }
        }
        // Either layout may be showing the developer page when its row goes away.
        .onChange(of: settings.developerMode) { _, enabled in
            if !enabled, path.contains(.developer) { path = SettingsRoute.withoutDeveloper(path) }
        }
    }

    private static var title: String {
        String(localized: "settings_title", defaultValue: "Settings")
    }

    private var compactBody: some View {
        NavigationStack(path: $path) {
            // The header sits outside the scroll view, so rows never pass under it or the status bar.
            VStack(spacing: 0) {
                header(inset: 16, background: Palette.bg)
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        AccountCard()
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
            }
            .background(Palette.bg.ignoresSafeArea())
            .navigationTitle(Self.title)
            .toolbar(.hidden, for: .navigationBar)
            .navigationDestination(for: SettingsRoute.self) { route in
                destination(route)
            }
        }
    }

    // The rail selects a section and the pane beside it shows that page in place, with no push.
    private var wideBody: some View {
        HStack(spacing: 0) {
            rail
                .frame(width: Self.railWidth)
            Rectangle()
                .fill(Palette.borderSubtle)
                .frame(width: 1)
                .ignoresSafeArea()
                .accessibilityHidden(true)
            NavigationStack(path: pushedPath) {
                destination(selectedRoute)
                    .navigationDestination(for: SettingsRoute.self) { route in
                        destination(route)
                    }
            }
            .frame(maxWidth: .infinity)
        }
    }

    private var selectedRoute: SettingsRoute {
        SettingsRoute.selected(in: path)
    }

    private var pushedPath: Binding<[SettingsRoute]> {
        Binding(
            get: { SettingsRoute.pushed(in: path) },
            set: { path = [selectedRoute] + $0 }
        )
    }

    private func header(inset: CGFloat, background: Color) -> some View {
        HStack(spacing: 8) {
            Text(Self.title)
                .scaledFont(size: 28, weight: .semibold, relativeTo: .largeTitle)
                .tracking(-0.4)
                .foregroundStyle(Palette.text)
                .lineLimit(1)
                .minimumScaleFactor(0.7)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 8)
            if closable {
                SettingsCloseButton { dismiss() }
                    .background(Palette.surface, in: Circle())
                    .overlay(Circle().strokeBorder(Palette.borderSubtle, lineWidth: 1))
            }
        }
        .padding(.horizontal, inset)
        .padding(.vertical, 8)
        .background(background)
    }

    private var rail: some View {
        VStack(spacing: 0) {
            header(inset: 12, background: Palette.surface)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    AccountCard(fill: Palette.bg)
                    railRoutes
                        .padding(.top, 14)
                    railActions
                        .padding(.top, 12)
                    versionLine
                        .padding(.top, 4)
                }
                .padding(.horizontal, 12)
                .padding(.bottom, 16)
            }
            .scrollBounceBehavior(.basedOnSize)
        }
        .background(Palette.surface.ignoresSafeArea())
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

    private var railRoutes: some View {
        VStack(spacing: 2) {
            ForEach(homeRoutes, id: \.self) { route in
                railButton(route, value: summary(route))
            }
        }
    }

    private func railButton(_ route: SettingsRoute, value: String?) -> some View {
        Button { path = [route] } label: {
            SettingsRailRow(systemImage: route.systemImage, title: route.title, value: value,
                            selected: route == selectedRoute)
        }
        .buttonStyle(.plain)
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
        case .subscription: return SubscriptionText.line(session.subscription).text
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
        case .subscription: SubscriptionPage()
        }
    }

    private var accountActions: some View {
        VStack(spacing: 8) {
            ForEach([SettingsRoute.subscription, .devices], id: \.self) { route in
                NavigationLink(value: route) {
                    SettingsNavRow(systemImage: route.systemImage, title: route.title, value: summary(route))
                }
                .buttonStyle(.plain)
            }
            signOutButton(SettingsNavRow(systemImage: Self.signOutImage, title: Self.signOutTitle,
                                         tint: Palette.error, showsChevron: false))
            DeleteAccountButton(row: SettingsNavRow(systemImage: Self.deleteImage, title: Self.deleteTitle,
                                                    tint: Palette.error, showsChevron: false),
                                onSignedOut: { dismiss() })
        }
    }

    // Desktop's account actions sit under a rule at the foot of the section list.
    private var railActions: some View {
        VStack(spacing: 2) {
            railButton(.subscription, value: summary(.subscription))
            railButton(.devices, value: nil)
            signOutButton(SettingsRailRow(systemImage: Self.signOutImage, title: Self.signOutTitle, tint: Palette.error))
            DeleteAccountButton(row: SettingsRailRow(systemImage: Self.deleteImage, title: Self.deleteTitle,
                                                     tint: Palette.error),
                                onSignedOut: { dismiss() })
        }
        .padding(.top, 12)
        .overlay(alignment: .top) {
            Rectangle()
                .fill(Palette.borderSubtle)
                .frame(height: 1)
                .accessibilityHidden(true)
        }
    }

    // Anchored on the row itself, so a wide window's popover points at what was tapped.
    private func signOutButton<Row: View>(_ row: Row) -> some View {
        Button { confirmingSignOut = true } label: {
            row
                .opacity(session.signingOut ? 0.5 : 1)
                .overlay(alignment: .trailing) {
                    if session.signingOut {
                        ProgressView()
                            .tint(Palette.error)
                            .padding(.trailing, 14)
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

    private static var signOutTitle: String {
        String(localized: "settings_signout", defaultValue: "Sign Out")
    }

    private static var deleteTitle: String {
        String(localized: "settings_delete_account", defaultValue: "Delete Account")
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
private func previewSettings(developer: Bool = false, path: [SettingsRoute] = []) -> some View {
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
    return SettingsView(path: path)
        .environment(session)
        .environment(PurchaseStore(core: core, session: session, store: FakeStoreClient()))
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

#Preview("Deep link, landscape", traits: .landscapeLeft) {
    previewSettings(path: [.startup])
}
#endif
