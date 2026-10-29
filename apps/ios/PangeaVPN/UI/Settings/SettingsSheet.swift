import SwiftUI

struct SettingsSheet: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(SessionStore.self) private var session
    @State private var signingOut = false

    private static let hubMethods = ["directIp", "reality", "shadowsocks", "fronted", "normal"]

    init() {}

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                Text(String(localized: "settings_title", defaultValue: "Settings"))
                    .scaledFont(size: 26, weight: .light, relativeTo: .title)
                    .tracking(-0.6)
                    .foregroundStyle(Palette.text)
                    .accessibilityAddTraits(.isHeader)
                if let message = settings.error {
                    Text(message)
                        .scaledFont(size: 13, relativeTo: .footnote)
                        .foregroundStyle(Palette.error)
                        .padding(.top, 10)
                }
                transportSection
                censorshipSection
                networkSection
                killSwitchSection
                accountSection
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 20)
            .padding(.top, 24)
            .padding(.bottom, 28)
        }
        .background(Palette.bg.ignoresSafeArea())
        .presentationDragIndicator(.visible)
        .task {
            if !settings.loaded { await settings.load() }
        }
    }

    private var transportSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(
                title: String(localized: "settings_transport_heading", defaultValue: "Censorship Bypass"),
                description: String(localized: "settings_transport_description", defaultValue: "How PangeaVPN disguises your traffic.")
            )
            VStack(spacing: 9) {
                ForEach(Transport.allCases, id: \.self) { transport in
                    ChoiceRow(
                        TransportLabels.choice(transport),
                        selected: settings.settings.preferredTransport == transport.rawValue
                    ) {
                        Task { await settings.setPreferredTransport(transport) }
                    }
                }
            }
        }
    }

    private var censorshipSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(
                title: String(localized: "settings_censorship_heading", defaultValue: "Provisioning"),
                description: String(localized: "settings_censorship_description", defaultValue: "Ways to fetch your VPN profile, tried in the order listed. Keep at least one on.")
            )
            VStack(spacing: 9) {
                ForEach(Self.hubMethods, id: \.self) { method in
                    StoreSwitchRow(
                        title: HubMethodLabels.title(method),
                        value: Self.isEnabled(method, in: settings.settings.hubMethods),
                        hint: HubMethodLabels.hint(method),
                        apply: { value in await settings.setHubMethod(method, enabled: value) }
                    )
                }
                StoreSwitchRow(
                    title: String(localized: "settings_censorship_deaddrop_title", defaultValue: "Recover addresses"),
                    value: settings.settings.deadDrop,
                    hint: String(localized: "settings_censorship_deaddrop_hint", defaultValue: "Fetches a signed address list from a public host, checked on your device. Last resort, and it carries no account details."),
                    apply: { value in await settings.setDeadDrop(value) }
                )
            }
        }
    }

    private var networkSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            SettingsSection(
                title: String(localized: "settings_network_heading", defaultValue: "Network"),
                description: String(localized: "settings_network_description", defaultValue: "Tunnel settings and fixes for restrictive Wi-Fi.")
            )
            StoreSwitchRow(
                title: String(localized: "settings_network_allowlan_title", defaultValue: "Allow LAN"),
                value: settings.settings.allowLan,
                hint: String(localized: "settings_network_allowlan_hint", defaultValue: "Lets local traffic (router, printers, captive portals) skip the tunnel. Turn it on if Wi-Fi keeps dropping to \"No internet\". Applies on next connect."),
                apply: { value in await settings.setAllowLan(value) }
            )
            CommittedField(
                label: String(localized: "settings_network_dns_title", defaultValue: "Custom DNS"),
                hint: String(localized: "settings_network_dns_hint", defaultValue: "IPv4 addresses, comma-separated. Blank uses the VPN default. Applies on next connect."),
                placeholder: String(localized: "settings_network_dns_placeholder", defaultValue: "1.1.1.1, 1.0.0.1"),
                initial: settings.settings.customDns.joined(separator: ", "),
                keyboard: .numbersAndPunctuation,
                transform: { $0 },
                onCommit: { value in Task { await settings.setCustomDns(value) } }
            )
            CommittedField(
                label: String(localized: "settings_network_mtu_title", defaultValue: "Tunnel MTU"),
                hint: String(localized: "settings_network_mtu_hint", defaultValue: "Packet size through the tunnel. Lower it if pages hang on a network that otherwise works. Default 1380. Applies on next connect."),
                placeholder: "",
                initial: String(settings.settings.mtu),
                keyboard: .numberPad,
                transform: SettingsInput.asciiDigits,
                onCommit: { value in
                    guard let mtu = Int(value) else { return }
                    Task { await settings.setMtu(mtu) }
                }
            )
        }
    }

    private var killSwitchSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            Spacer().frame(height: 26)
            SwitchRow(
                String(localized: "settings_kill_switch", defaultValue: "Kill switch"),
                isOn: Binding(
                    get: { settings.killSwitch },
                    set: { settings.setKillSwitch($0) }
                ),
                hint: String(localized: "settings_kill_switch_hint", defaultValue: "Block all traffic outside the tunnel. Takes effect on the next connect.")
            )
        }
    }

    private var accountSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            Spacer().frame(height: 26)
            GhostButton(
                String(localized: "settings_signout", defaultValue: "Sign Out"),
                accent: Palette.error
            ) {
                signOut()
            }
            .disabled(signingOut)
            .opacity(signingOut ? 0.5 : 1)
            Text(verbatim: "v\(Self.versionName)")
                .scaledFont(size: 11, relativeTo: .caption2)
                .foregroundStyle(Palette.textMuted)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 18)
        }
    }

    private func signOut() {
        guard !signingOut else { return }
        signingOut = true
        Task {
            await session.signOut()
            signingOut = false
        }
    }

    private static var versionName: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
    }

    private static func isEnabled(_ method: String, in methods: HubMethods) -> Bool {
        switch method {
        case "directIp": return methods.directIp
        case "reality": return methods.reality
        case "shadowsocks": return methods.shadowsocks
        case "fronted": return methods.fronted
        case "normal": return methods.normal
        default: return false
        }
    }
}

private struct SettingsSection: View {
    let title: String
    let description: String

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Spacer().frame(height: 26)
            SectionLabel(title)
            Text(description)
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(Palette.textMuted)
                .padding(.top, 6)
                .padding(.bottom, 11)
        }
    }
}

private struct CommittedField: View {
    let label: String
    let hint: String
    let placeholder: String
    let initial: String
    let keyboard: UIKeyboardType
    let transform: (String) -> String
    let onCommit: (String) -> Void

    @State private var text: String

    init(
        label: String,
        hint: String,
        placeholder: String,
        initial: String,
        keyboard: UIKeyboardType,
        transform: @escaping (String) -> String,
        onCommit: @escaping (String) -> Void
    ) {
        self.label = label
        self.hint = hint
        self.placeholder = placeholder
        self.initial = initial
        self.keyboard = keyboard
        self.transform = transform
        self.onCommit = onCommit
        _text = State(initialValue: initial)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Spacer().frame(height: 14)
            HStack {
                SectionLabel(label)
                Spacer()
                if text != initial {
                    Button(String(localized: "settings_save", defaultValue: "Save")) { onCommit(text) }
                        .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                        .foregroundStyle(Palette.accent)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                }
            }
            SettingsField(
                text: Binding(get: { text }, set: { text = transform($0) }),
                placeholder: placeholder,
                keyboard: keyboard
            )
            .onSubmit { if text != initial { onCommit(text) } }
            .padding(.top, 8)
            Text(hint)
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(Palette.textMuted)
                .padding(.top, 6)
        }
        .onChange(of: initial) { _, value in text = value }
    }
}

#if DEBUG
@MainActor
private func previewSheet(error: String? = nil) -> some View {
    let core = FakeCore()
    let settings = SettingsStore(core: core)
    settings.error = error
    let session = SessionStore(core: core, tunnel: FakeTunnel())
    return SettingsSheet()
        .environment(settings)
        .environment(session)
}

#Preview("Settings") {
    previewSheet()
}

#Preview("Settings error, dark") {
    previewSheet(error: "At least one connection method must stay on.")
        .preferredColorScheme(.dark)
}
#endif
