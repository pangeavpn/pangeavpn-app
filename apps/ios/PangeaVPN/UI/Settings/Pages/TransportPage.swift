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
