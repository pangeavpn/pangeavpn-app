import SwiftUI

// The core quietly swaps an out-of-range MTU for its default, so the page refuses it first.
enum TunnelMtu {
    static let range = 1280...1420

    static func parse(_ text: String) -> Int? {
        let trimmed = text.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty,
              trimmed.unicodeScalars.allSatisfy({ ("0"..."9").contains($0) }),
              let value = Int(trimmed), range.contains(value) else { return nil }
        return value
    }
}

struct NetworkPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ConnectionStore.self) private var connection
    @Environment(ToastCenter.self) private var toasts
    @State private var customChosen = false
    @State private var dnsSaving = false
    @State private var dnsError: String?
    @State private var mtuError: String?
    // Bumped after a save, so a field showing an equivalent spelling resets to the stored value.
    @State private var dnsFieldID = 0
    @State private var mtuFieldID = 0

    init() {}

    var body: some View {
        SettingsPage(.network, description: String(localized: "settings_network_description", defaultValue: "Tunnel settings and fixes for restrictive Wi-Fi.")) {
            SettingsCard {
                StoreSwitchRow(
                    title: String(localized: "settings_network_allowlan_title", defaultValue: "Allow LAN"),
                    value: settings.settings.allowLan,
                    hint: String(localized: "settings_network_allowlan_hint", defaultValue: "Lets local traffic (router, printers, captive portals) skip the tunnel. Turn it on if Wi-Fi keeps dropping to \"No internet\". Applies on next connect."),
                    apply: { value in await setAllowLan(value) }
                )
                StoreSwitchRow(
                    title: String(localized: "network_pq_title", defaultValue: "Post-quantum key"),
                    value: settings.settings.postQuantum,
                    hint: String(localized: "network_pq_hint", defaultValue: "Adds a quantum-resistant key to the tunnel, so traffic recorded today can't be decrypted later. Applies on next connect."),
                    apply: { value in await setPostQuantum(value) }
                )
            }
            dnsCard
            SettingsCard {
                CommittedField(
                    label: String(localized: "settings_network_mtu_title", defaultValue: "Tunnel MTU"),
                    hint: String(localized: "settings_network_mtu_hint", defaultValue: "Packet size through the tunnel. Lower it if pages hang on a network that otherwise works. Default 1380. Applies on next connect."),
                    placeholder: String(AppSettings().mtu),
                    initial: String(settings.settings.mtu),
                    keyboard: .numberPad,
                    transform: SettingsInput.asciiDigits,
                    onCommit: commitMtu
                )
                .id(mtuFieldID)
                if let mtuError {
                    InfoNote(mtuError, systemImage: "exclamationmark.circle", tint: Palette.error)
                }
            }
        }
        .task {
            if !settings.loaded { await settings.load() }
        }
    }

    private var dnsCard: some View {
        SettingsCard(Self.providerTitle) {
            DnsProviderMenu(title: Self.providerTitle, selection: dnsChoice, choose: choose)
                .disabled(dnsSaving)
            Text(String(localized: "network_dns_provider_hint", defaultValue: "Pick a resolver, or enter your own. Filtering comes from that service. Applies on next connect."))
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(Palette.textMuted)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, 4)
            if dnsChoice == .custom {
                CommittedField(
                    label: String(localized: "settings_network_dns_title", defaultValue: "Custom DNS"),
                    hint: String(localized: "settings_network_dns_hint", defaultValue: "IPv4 addresses, comma-separated. Blank uses the VPN default. Applies on next connect."),
                    placeholder: String(localized: "settings_network_dns_placeholder", defaultValue: "1.1.1.1, 1.0.0.1"),
                    initial: settings.settings.customDns.joined(separator: ", "),
                    keyboard: .numbersAndPunctuation,
                    transform: DnsPresets.typed,
                    onCommit: commitCustomDns
                )
                .id(dnsFieldID)
                .padding(.top, 6)
                if let dnsError {
                    InfoNote(dnsError, systemImage: "exclamationmark.circle", tint: Palette.error)
                }
            }
        }
    }

    private static var providerTitle: String {
        String(localized: "network_dns_provider_title", defaultValue: "DNS Provider")
    }

    // Custom stays picked while its field is empty or matches a preset, as on desktop.
    private var dnsChoice: DnsChoice {
        customChosen ? .custom : DnsPresets.choice(for: settings.settings.customDns)
    }

    // The saved VPN profile carries Allow LAN too, so it is pushed like the kill switch pages do.
    private func setAllowLan(_ enabled: Bool) async {
        await settings.setAllowLan(enabled)
        report(enabled
            ? String(localized: "network_allowlan_on", defaultValue: "Allow LAN enabled. Reconnect for it to take effect.")
            : String(localized: "network_allowlan_off", defaultValue: "Allow LAN disabled. Reconnect for it to take effect."))
        guard settings.error == nil else { return }
        await TunnelProfileSync.push(settings: settings, connection: connection)
    }

    private func setPostQuantum(_ enabled: Bool) async {
        await settings.setPostQuantum(enabled)
        report(enabled
            ? String(localized: "network_pq_on", defaultValue: "Post-quantum key enabled. Reconnect for it to take effect.")
            : String(localized: "network_pq_off", defaultValue: "Post-quantum key disabled. Reconnect for it to take effect."))
    }

    private func choose(_ choice: DnsChoice) {
        guard !dnsSaving else { return }
        dnsError = nil
        guard let servers = DnsPresets.servers(for: choice) else {
            customChosen = true
            return
        }
        customChosen = false
        if servers != settings.settings.customDns { saveDns(servers) }
    }

    private func commitCustomDns(_ text: String) {
        guard let servers = DnsPresets.normalize(text) else {
            dnsError = String(localized: "settings_network_dns_invalid", defaultValue: "Enter one or more valid IPv4 DNS addresses separated by commas.")
            return
        }
        dnsError = nil
        saveDns(servers)
    }

    private func saveDns(_ servers: [String]) {
        dnsSaving = true
        Task {
            await settings.setCustomDns(servers.joined(separator: ", "))
            dnsSaving = false
            if settings.error == nil {
                customChosen = false
                dnsFieldID += 1
            }
            let stored = settings.settings.customDns
            report(stored.isEmpty
                ? String(localized: "network_dns_default_saved", defaultValue: "DNS restored to the VPN default. Takes effect on next connect.")
                : String(format: String(localized: "network_dns_saved", defaultValue: "DNS set to %@. Takes effect on next connect."), stored.joined(separator: ", ")))
        }
    }

    private func commitMtu(_ text: String) {
        guard let mtu = TunnelMtu.parse(text) else {
            let format = String(localized: "network_mtu_invalid", defaultValue: "Enter a whole number between %lld and %lld.")
            mtuError = String(format: format, TunnelMtu.range.lowerBound, TunnelMtu.range.upperBound)
            return
        }
        mtuError = nil
        Task {
            await settings.setMtu(mtu)
            if settings.error == nil { mtuFieldID += 1 }
            let format = String(localized: "network_mtu_saved", defaultValue: "Tunnel MTU set to %lld. Takes effect on next connect.")
            report(String(format: format, settings.settings.mtu))
        }
    }

    // The store clears its error on every successful save, so a leftover one means this save failed.
    private func report(_ success: String) {
        if settings.error == nil {
            toasts.show(success, style: .success)
        } else {
            toasts.show(String(localized: "network_update_failed", defaultValue: "Failed to update setting."), style: .warning)
        }
    }
}

private struct DnsProviderMenu: View {
    let title: String
    let selection: DnsChoice
    let choose: (DnsChoice) -> Void

    var body: some View {
        Menu {
            Picker(title, selection: Binding(get: { selection }, set: { choose($0) })) {
                ForEach(DnsChoice.allCases, id: \.self) { choice in
                    Text(DnsPresets.title(choice)).tag(choice)
                }
            }
        } label: {
            HStack(spacing: 10) {
                Text(DnsPresets.title(selection))
                    .scaledFont(size: 15, relativeTo: .subheadline)
                    .foregroundStyle(Palette.text)
                    .multilineTextAlignment(.leading)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: "chevron.up.chevron.down")
                    .scaledFont(size: 12, weight: .semibold, relativeTo: .caption)
                    .foregroundStyle(Palette.textMuted)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 13)
            .frame(minHeight: 44)
            .background(Palette.surface)
            .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .accessibilityLabel(title)
        .accessibilityValue(DnsPresets.title(selection))
    }
}

extension SettingsSummary {
    static func network(_ s: AppSettings) -> String {
        let dns = DnsPresets.choice(for: s.customDns)
        let mtu = String(format: String(localized: "network_summary_mtu", defaultValue: "MTU %lld"), s.mtu)
        return joined([
            mtu,
            dns == .automatic ? "" : DnsPresets.title(dns),
            s.allowLan ? String(localized: "settings_network_allowlan_title", defaultValue: "Allow LAN") : "",
            s.postQuantum ? String(localized: "network_summary_pq", defaultValue: "PQ") : "",
        ])
    }
}

#if DEBUG
@MainActor
private func networkPreview(_ change: (inout AppSettings) -> Void) -> some View {
    let core = FakeCore()
    var config = AppSettings()
    change(&config)
    core.configResult = .success(config)
    let defaults = UserDefaults(suiteName: ProtectionPreview.suite) ?? .standard
    let tunnel = FakeTunnel()
    let settings = SettingsStore(core: core, defaults: defaults)
    let servers = ServersStore(core: core, defaults: defaults)
    let session = SessionStore(core: core, tunnel: tunnel)
    return NavigationStack {
        NetworkPage()
    }
    .toastOverlay()
    .environment(settings)
    .environment(ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session))
    .environment(ToastCenter())
}

#Preview("Network") {
    networkPreview { $0.customDns = ["1.1.1.1", "1.0.0.1"]; $0.postQuantum = true }
}

#Preview("Network, custom DNS, dark") {
    networkPreview { $0.customDns = ["9.9.9.10"]; $0.allowLan = true; $0.mtu = 1300 }
        .preferredColorScheme(.dark)
}
#endif
