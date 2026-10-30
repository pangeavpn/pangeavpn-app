import SwiftUI

struct StartupPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ConnectionStore.self) private var connection
    @Environment(ToastCenter.self) private var toasts

    init() {}

    var body: some View {
        SettingsPage(.startup, description: String(localized: "startup_description", defaultValue: "Automatic connection and reconnection.")) {
            SettingsCard {
                StoreSwitchRow(
                    title: Self.autoConnectTitle,
                    value: settings.autoConnect || settings.lockdown,
                    hint: String(localized: "startup_auto_connect_hint", defaultValue: "Connects when your iPhone starts or the network returns, and reconnects if the tunnel drops. Disconnecting in PangeaVPN pauses it until you connect again."),
                    apply: { await setAutoConnect($0) }
                )
                .disabled(settings.lockdown)
                if settings.lockdown {
                    InfoNote(String(localized: "startup_lockdown_keeps_auto", defaultValue: "Lockdown keeps Auto-connect on."), systemImage: "lock")
                } else if settings.autoConnect {
                    InfoNote(String(localized: "startup_auto_connect_note", defaultValue: "While it's on, iOS may turn the VPN back on if you switch it off in Control Center or iOS Settings. To stop it, disconnect in PangeaVPN."))
                }
            }
            SettingsCard {
                StoreSwitchRow(
                    title: Self.lockdownTitle,
                    value: settings.lockdown,
                    hint: String(localized: "startup_lockdown_hint", defaultValue: "Keeps the VPN on at all times. The Disconnect button is hidden until you turn Lockdown off."),
                    apply: { await setLockdown($0) }
                )
            }
        }
    }

    static var autoConnectTitle: String {
        String(localized: "startup_auto_connect_title", defaultValue: "Auto-connect")
    }

    static var lockdownTitle: String {
        String(localized: "startup_lockdown_title", defaultValue: "Lockdown")
    }

    // Lockdown already keeps Connect On Demand on, so the switch underneath it cannot change anything.
    private func setAutoConnect(_ enabled: Bool) async {
        guard !settings.lockdown, enabled != settings.autoConnect else { return }
        settings.setAutoConnect(enabled)
        if enabled {
            toasts.show(String(localized: "startup_toast_auto_on", defaultValue: "Auto-connect on. PangeaVPN reconnects after drops and when the network returns."), style: .success)
        } else {
            toasts.show(String(localized: "startup_toast_auto_off", defaultValue: "Auto-connect off."), style: .info)
        }
        await TunnelProfileSync.push(settings: settings, connection: connection)
    }

    private func setLockdown(_ enabled: Bool) async {
        guard enabled != settings.lockdown else { return }
        settings.setLockdown(enabled)
        if enabled {
            toasts.show(String(localized: "startup_toast_lockdown_on", defaultValue: "Lockdown on. The VPN stays on until you turn Lockdown off."), style: .success)
        } else {
            toasts.show(String(localized: "startup_toast_lockdown_off", defaultValue: "Lockdown off. You can disconnect again."), style: .info)
        }
        await TunnelProfileSync.push(settings: settings, connection: connection)
    }
}

extension SettingsSummary {
    static func startup(_ settings: SettingsStore) -> String {
        joined([
            settings.autoConnect || settings.lockdown ? StartupPage.autoConnectTitle : "",
            settings.lockdown ? StartupPage.lockdownTitle : "",
        ])
    }
}

#if DEBUG
#Preview("Startup") {
    ProtectionPreview.stage(StartupPage())
}

#Preview("Startup, Lockdown, dark") {
    ProtectionPreview.stage(StartupPage()) { $0.setLockdown(true) }
        .preferredColorScheme(.dark)
}
#endif
