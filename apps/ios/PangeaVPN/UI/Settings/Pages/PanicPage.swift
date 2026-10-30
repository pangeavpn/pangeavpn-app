import SwiftUI

struct PanicPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    @State private var confirming = false

    init() {}

    var body: some View {
        SettingsPage(.panic, description: String(localized: "panic_description", defaultValue: "A quick way to erase PangeaVPN from this iPhone and restore your network.")) {
            SettingsCard {
                SwitchRow(
                    String(localized: "panic_enable_title", defaultValue: "Enable panic wipe"),
                    isOn: Binding(get: { settings.panicWipe }, set: { setEnabled($0) }),
                    hint: String(localized: "panic_enable_hint", defaultValue: "Adds a “Wipe data” action to the PangeaVPN icon's long-press menu. Choosing it disconnects, erases all PangeaVPN data and the VPN profile from this iPhone, and returns to sign-in. Delete the app afterwards to remove it entirely. This can't be undone.")
                )
            }
            SettingsCard {
                wipeNowButton
                InfoNote(String(localized: "panic_now_note", defaultValue: "Runs the same wipe straight away, whether or not panic wipe is on."))
            }
        }
    }

    private var wipeNowButton: some View {
        GhostButton(String(localized: "panic_now_button", defaultValue: "Wipe Now"), accent: Palette.error) {
            confirming = true
        }
        .confirmationDialog(
            String(localized: "panic_confirm_title", defaultValue: "Wipe PangeaVPN now?"),
            isPresented: $confirming,
            titleVisibility: .visible
        ) {
            Button(String(localized: "panic_confirm_wipe", defaultValue: "Wipe"), role: .destructive) {
                PanicTrigger.shared.request()
            }
            Button(String(localized: "panic_confirm_cancel", defaultValue: "Cancel"), role: .cancel) {}
        } message: {
            Text(String(localized: "panic_confirm_message", defaultValue: "This disconnects, erases all PangeaVPN data and the VPN profile from this iPhone, and returns to sign-in. It can't be undone."))
        }
    }

    private func setEnabled(_ enabled: Bool) {
        settings.setPanicWipe(enabled)
        PanicShortcut.update(enabled: enabled)
        if enabled {
            toasts.show(String(localized: "panic_toast_on", defaultValue: "Panic wipe armed."), style: .success)
        } else {
            toasts.show(String(localized: "panic_toast_off", defaultValue: "Panic wipe off."), style: .info)
        }
    }
}

extension SettingsSummary {
    static func panic(_ settings: SettingsStore) -> String {
        settings.panicWipe ? String(localized: "panic_summary_on", defaultValue: "Enabled") : off()
    }
}

#if DEBUG
#Preview("Panic wipe") {
    NavigationStack {
        PanicPage()
    }
    .environment(SettingsStore(core: FakeCore()))
    .environment(ToastCenter())
}
#endif
