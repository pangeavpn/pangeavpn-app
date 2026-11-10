import SwiftUI

struct TransportPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    @State private var pending: Transport?

    init() {}

    var body: some View {
        SettingsPage(.transport, description: String(localized: "settings_transport_description", defaultValue: "How PangeaVPN disguises your traffic.")) {
            SettingsCard {
                VStack(spacing: 9) {
                    ForEach(TransportLabels.pickerOrder, id: \.self) { transport in
                        ChoiceRow(
                            TransportLabels.choice(transport),
                            selected: selected == transport,
                            hint: TransportLabels.description(transport)
                        ) {
                            select(transport)
                        }
                    }
                }
                .disabled(pending != nil)
                .accessibilityElement(children: .contain)
                .accessibilityLabel(SettingsRoute.transport.title)
            }
            SettingsCard {
                StoreSwitchRow(
                    title: String(localized: "settings_transport_race_title", defaultValue: "Fast fallback"),
                    value: settings.settings.transportRace,
                    hint: String(localized: "settings_transport_race_hint", defaultValue: "With Automatic, tries several disguises at once instead of waiting for each blocked one to time out. Applies on next connect."),
                    apply: { value in await setTransportRace(value) }
                )
                // A pinned transport never races, so the switch only means something under Automatic.
                .disabled(!raceApplies)
                .opacity(raceApplies ? 1 : 0.45)
            }
            InfoNote(String(localized: "bypass_regions_note", defaultValue: "Regions that don't offer the selected method are hidden from the region list."))
        }
        .task {
            if !settings.loaded { await settings.load() }
        }
    }

    // Shows the tap at once, so the list never lags behind the finger while the core saves.
    private var selected: Transport {
        pending ?? TransportLabels.selected(settings.settings.preferredTransport)
    }

    private var raceApplies: Bool {
        selected == .auto && pending == nil
    }

    private func setTransportRace(_ enabled: Bool) async {
        await settings.setTransportRace(enabled)
        if settings.error == nil {
            toasts.show(enabled
                ? String(localized: "settings_transport_race_on", defaultValue: "Fast fallback enabled. Reconnect for it to take effect.")
                : String(localized: "settings_transport_race_off", defaultValue: "Fast fallback disabled. Reconnect for it to take effect."),
                style: .success)
        } else {
            toasts.show(String(localized: "network_update_failed", defaultValue: "Failed to update setting."), style: .warning)
        }
    }

    private func select(_ transport: Transport) {
        guard pending == nil, transport != selected else { return }
        pending = transport
        Task {
            await settings.setPreferredTransport(transport)
            pending = nil
            if settings.error == nil {
                toasts.show(String(localized: "bypass_updated", defaultValue: "Connection method updated. Reconnect for it to take effect."), style: .success)
            } else {
                toasts.show(String(localized: "network_update_failed", defaultValue: "Failed to update setting."), style: .warning)
            }
        }
    }
}

extension SettingsSummary {
    static func transport(_ s: AppSettings) -> String {
        TransportLabels.choice(TransportLabels.selected(s.preferredTransport))
    }
}

#if DEBUG
@MainActor
private func transportPreview(_ transport: Transport) -> some View {
    let core = FakeCore()
    var config = AppSettings()
    config.preferredTransport = transport.rawValue
    core.configResult = .success(config)
    return NavigationStack {
        TransportPage()
    }
    .toastOverlay()
    .environment(SettingsStore(core: core))
    .environment(ToastCenter())
}

#Preview("Censorship Bypass") {
    transportPreview(.auto)
}

#Preview("Censorship Bypass, REALITY, dark") {
    transportPreview(.reality)
        .preferredColorScheme(.dark)
}
#endif
