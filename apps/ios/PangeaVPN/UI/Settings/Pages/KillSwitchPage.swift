import SwiftUI

struct KillSwitchPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ConnectionStore.self) private var connection
    @Environment(ToastCenter.self) private var toasts
    @State private var confirming = false

    init() {}

    var body: some View {
        SettingsPage(.killSwitch, description: String(localized: "kill_switch_description", defaultValue: "How PangeaVPN keeps your traffic from leaking outside the VPN.")) {
            SettingsCard(String(localized: "kill_switch_always_on", defaultValue: "Always on")) {
                AlwaysOnList(restarts: settings.autoConnect || settings.lockdown)
            }
            SettingsCard {
                SwitchRow(
                    String(localized: "kill_switch_strict_title", defaultValue: "Strict kill switch"),
                    isOn: Binding(get: { settings.killSwitch }, set: { request($0) }),
                    hint: String(localized: "kill_switch_strict_hint", defaultValue: "Sends every connection through the VPN, including ones iOS would otherwise let out. Applies on next connect.")
                )
                if settings.killSwitch {
                    ForEach(Self.caveats, id: \.self) { caveat in
                        InfoNote(caveat, systemImage: "exclamationmark.triangle", tint: Palette.textSecondary)
                    }
                }
            }
        }
        .alert(String(localized: "kill_switch_strict_confirm_title", defaultValue: "Turn on strict kill switch?"), isPresented: $confirming) {
            Button(String(localized: "kill_switch_strict_confirm_on", defaultValue: "Turn On")) {
                Task { await setStrict(true) }
            }
            Button(String(localized: "kill_switch_strict_confirm_cancel", defaultValue: "Cancel"), role: .cancel) {}
        } message: {
            Text(Self.caveats.map { "\u{2022} " + $0 }.joined(separator: "\n"))
        }
    }

    static var caveats: [String] {
        [
            String(localized: "kill_switch_caveat_update", defaultValue: "Updating PangeaVPN while connected can leave your iPhone offline until it restarts."),
            String(localized: "kill_switch_caveat_lan", defaultValue: "AirDrop, AirPlay and printing need Allow LAN."),
            String(localized: "kill_switch_caveat_carplay", defaultValue: "CarPlay and Personal Hotspot may not work."),
            String(localized: "kill_switch_caveat_system", defaultValue: "Some Apple system services may still bypass the VPN."),
        ]
    }

    // Turning it on waits for the warning; the switch stays off until the user confirms.
    private func request(_ enabled: Bool) {
        if enabled && !settings.killSwitch {
            confirming = true
        } else {
            Task { await setStrict(enabled) }
        }
    }

    private func setStrict(_ enabled: Bool) async {
        guard enabled != settings.killSwitch else { return }
        settings.setKillSwitch(enabled)
        if enabled {
            toasts.show(String(localized: "kill_switch_toast_on", defaultValue: "Strict kill switch on. Applies on next connect."), style: .success)
        } else {
            toasts.show(String(localized: "kill_switch_toast_off", defaultValue: "Strict kill switch off. Applies on next connect."), style: .info)
        }
        await TunnelProfileSync.push(settings: settings, connection: connection)
    }
}

// What protects traffic whatever the switches say; the restart line depends on Auto-connect.
private struct AlwaysOnList: View {
    let restarts: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            GuaranteeRow(String(localized: "kill_switch_always_held", defaultValue: "Traffic is held inside the VPN while it reconnects or switches servers."), active: true)
            GuaranteeRow(String(localized: "kill_switch_always_routes", defaultValue: "All traffic is routed into the VPN, and IPv6 is blocked so it can't leak."), active: true)
            GuaranteeRow(String(localized: "kill_switch_always_restart", defaultValue: "If iOS stops the VPN, it restarts itself when Auto-connect is on."), active: restarts)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }
}

private struct GuaranteeRow: View {
    private let text: String
    private let active: Bool

    init(_ text: String, active: Bool) {
        self.text = text
        self.active = active
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: active ? "checkmark.circle.fill" : "circle")
                .scaledFont(size: 14, relativeTo: .subheadline)
                .foregroundStyle(active ? Palette.connected : Palette.textMuted)
                .accessibilityHidden(true)
            Text(text)
                .scaledFont(size: 14, relativeTo: .subheadline)
                .foregroundStyle(active ? Palette.text : Palette.textMuted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

// Saves the new profile to the VPN configuration, once the core settings it carries are known.
@MainActor
enum TunnelProfileSync {
    static func push(settings: SettingsStore, connection: ConnectionStore) async {
        if !settings.loaded { await settings.load() }
        guard settings.loaded else { return }
        await connection.applyProfile()
    }
}

extension SettingsSummary {
    static func killSwitch(_ settings: SettingsStore) -> String {
        settings.killSwitch
            ? String(localized: "kill_switch_summary_strict", defaultValue: "Strict")
            : String(localized: "kill_switch_always_on", defaultValue: "Always on")
    }
}

#if DEBUG
// The stores every protection page reads, on throwaway defaults so previews never touch the app group.
@MainActor
enum ProtectionPreview {
    static let suite = "org.pangeavpn.preview.protection"

    static func stage<Page: View>(_ page: Page, configure: (SettingsStore) -> Void = { _ in }) -> some View {
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        let core = FakeCore()
        let tunnel = FakeTunnel()
        let settings = SettingsStore(core: core, defaults: defaults)
        configure(settings)
        let servers = ServersStore(core: core, defaults: defaults)
        let session = SessionStore(core: core, tunnel: tunnel)
        return NavigationStack { page }
            .toastOverlay()
            .environment(settings)
            .environment(ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session))
            .environment(ToastCenter())
    }
}

#Preview("Kill switch") {
    ProtectionPreview.stage(KillSwitchPage())
}

#Preview("Kill switch, strict, dark") {
    ProtectionPreview.stage(KillSwitchPage()) { $0.setKillSwitch(true) }
        .preferredColorScheme(.dark)
}
#endif
