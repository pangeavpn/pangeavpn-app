import SwiftUI
import UIKit

struct ProvisioningPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    @State private var status = HubStatus.none
    @State private var results: [String: HubTestResult] = [:]
    @State private var testing: String?
    @State private var pending: [String: Bool] = [:]
    @State private var confirmingNormalTest = false

    init() {}

    var body: some View {
        SettingsPage(.provisioning, description: String(localized: "settings_censorship_description", defaultValue: "Ways to fetch your VPN profile, tried in the order listed. Keep at least one on.")) {
            ActiveMethodLine(text: HubMethodLabels.activeText(status), live: !(status.active ?? "").isEmpty)
            SettingsCard {
                ForEach(HubMethodLabels.order, id: \.self) { method in
                    row(method)
                }
            }
            SettingsCard {
                Text(String(localized: "provisioning_deaddrop_description", defaultValue: "If the methods above run out of addresses, the app can fetch replacements from a signed public file."))
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.textMuted)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 4)
                StoreSwitchRow(
                    title: String(localized: "settings_censorship_deaddrop_title", defaultValue: "Recover addresses"),
                    value: settings.settings.deadDrop,
                    hint: String(localized: "settings_censorship_deaddrop_hint", defaultValue: "Fetches a signed address list from a public host, checked on your device. Last resort, and it carries no account details."),
                    apply: { value in await setDeadDrop(value) }
                )
            }
        }
        .task {
            if !settings.loaded { await settings.load() }
            status = await settings.hubStatus()
        }
        .alert(
            String(localized: "provisioning_normal_test_title", defaultValue: "Test the normal connection?"),
            isPresented: $confirmingNormalTest
        ) {
            Button(String(localized: "provisioning_normal_test_confirm", defaultValue: "Test anyway")) {
                runTest("normal")
            }
            Button(String(localized: "provisioning_normal_test_cancel", defaultValue: "Cancel"), role: .cancel) {}
        } message: {
            Text(String(localized: "provisioning_normal_test_message", defaultValue: "It's off because it shows our domain to anyone watching your network. Testing it does the same, once."))
        }
    }

    private func row(_ method: String) -> some View {
        let on = isOn(method)
        return HubMethodRow(
            title: HubMethodLabels.title(method),
            hint: HubMethodLabels.hint(method),
            isOn: on,
            locked: on && enabledCount == 1,
            inUse: status.active == method,
            result: results[method],
            testing: testing == method,
            testsLocked: testing != nil,
            onToggle: { setMethod(method, enabled: $0) },
            onTest: { requestTest(method) }
        )
    }

    // Counts taps still saving, so two quick switches can never turn every method off.
    private func isOn(_ method: String) -> Bool {
        pending[method] ?? HubMethodLabels.isEnabled(method, in: settings.settings.hubMethods)
    }

    private var enabledCount: Int {
        HubMethodLabels.order.filter { isOn($0) }.count
    }

    private func setMethod(_ method: String, enabled: Bool) {
        guard pending[method] == nil else { return }
        pending[method] = enabled
        Task {
            await settings.setHubMethod(method, enabled: enabled)
            pending[method] = nil
            guard settings.error == nil else {
                toasts.show(String(localized: "network_update_failed", defaultValue: "Failed to update setting."), style: .warning)
                return
            }
            let format = enabled
                ? String(localized: "provisioning_method_on", defaultValue: "%@ — on")
                : String(localized: "provisioning_method_off", defaultValue: "%@ — off")
            toasts.show(String(format: format, HubMethodLabels.title(method)), style: .success)
        }
    }

    private func setDeadDrop(_ enabled: Bool) async {
        await settings.setDeadDrop(enabled)
        guard settings.error == nil else {
            toasts.show(String(localized: "network_update_failed", defaultValue: "Failed to update setting."), style: .warning)
            return
        }
        let text = enabled
            ? String(localized: "provisioning_deaddrop_on", defaultValue: "Address recovery on — used only when every method above has failed.")
            : String(localized: "provisioning_deaddrop_off", defaultValue: "Address recovery off.")
        toasts.show(text, style: .success)
    }

    // Testing a switched-off normal connection still puts our domain on the wire, so it asks first.
    private func requestTest(_ method: String) {
        guard testing == nil else { return }
        if method == "normal", !isOn(method) {
            confirmingNormalTest = true
        } else {
            runTest(method)
        }
    }

    private func runTest(_ method: String) {
        guard testing == nil else { return }
        testing = method
        results[method] = nil
        Task {
            let result = await settings.testHubMethod(method)
            results[method] = result
            testing = nil
            UIAccessibility.post(notification: .announcement, argument: HubMethodLabels.testText(result))
            status = await settings.hubStatus()
        }
    }
}

private struct ActiveMethodLine: View {
    let text: String
    let live: Bool

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(live ? Palette.connected : Palette.off)
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            Text(text)
                .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                .foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 4)
        .accessibilityElement(children: .combine)
    }
}

private struct HubMethodRow: View {
    let title: String
    let hint: String
    let isOn: Bool
    let locked: Bool
    let inUse: Bool
    let result: HubTestResult?
    let testing: Bool
    let testsLocked: Bool
    let onToggle: (Bool) -> Void
    let onTest: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .top, spacing: 12) {
                copy
                Toggle(title, isOn: Binding(get: { isOn }, set: onToggle))
                    .labelsHidden()
                    .tint(Palette.accent)
                    .disabled(locked)
                    .accessibilityHint(locked ? Self.lastMethod : hint)
            }
            HStack(spacing: 10) {
                testButton
                resultLine
            }
        }
        .padding(.horizontal, 14)
        .padding(.top, 10)
        .padding(.bottom, 4)
        .background(Palette.surface)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
        .accessibilityElement(children: .contain)
    }

    private static var lastMethod: String {
        String(localized: "settings_censorship_last_method", defaultValue: "At least one connection method must stay on.")
    }

    private var copy: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 8) {
                Text(title)
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                    .foregroundStyle(Palette.text)
                if inUse {
                    Text(String(localized: "provisioning_in_use", defaultValue: "In use"))
                        .scaledFont(size: 10, weight: .bold, relativeTo: .caption2)
                        .foregroundStyle(Palette.connected)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 2)
                        .background(Palette.connected.opacity(0.15), in: Capsule())
                }
            }
            Text(hint)
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(Palette.textMuted)
                .fixedSize(horizontal: false, vertical: true)
            if locked {
                Text(Self.lastMethod)
                    .scaledFont(size: 12, weight: .medium, relativeTo: .caption)
                    .foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var testButton: some View {
        Button(action: onTest) {
            Text(testing ? Self.testingLabel : String(localized: "provisioning_test", defaultValue: "Test"))
                .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                .foregroundStyle(Palette.accent)
                .padding(.horizontal, 14)
                .padding(.vertical, 6)
                .overlay(Capsule().strokeBorder(Palette.accent.opacity(0.5), lineWidth: 1))
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(testsLocked)
        .opacity(testsLocked && !testing ? 0.45 : 1)
        .accessibilityLabel(testing ? Self.testingLabel : String(format: String(localized: "provisioning_test_method", defaultValue: "Test %@"), title))
    }

    private static var testingLabel: String {
        String(localized: "provisioning_testing", defaultValue: "Testing…")
    }

    @ViewBuilder
    private var resultLine: some View {
        if let result, !testing {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Image(systemName: Self.symbol(HubMethodLabels.tone(result)))
                    .scaledFont(size: 12, weight: .semibold, relativeTo: .caption)
                    .foregroundStyle(Self.tint(HubMethodLabels.tone(result)))
                    .accessibilityHidden(true)
                Text(HubMethodLabels.testText(result))
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private static func symbol(_ tone: HubTestTone) -> String {
        switch tone {
        case .ok: return "checkmark.circle.fill"
        case .unavailable: return "minus.circle"
        case .failed: return "xmark.circle.fill"
        }
    }

    private static func tint(_ tone: HubTestTone) -> Color {
        switch tone {
        case .ok: return Palette.connected
        case .unavailable: return Palette.connecting
        case .failed: return Palette.error
        }
    }
}

extension SettingsSummary {
    static func provisioning(_ s: AppSettings) -> String {
        joined(HubMethodLabels.enabled(in: s.hubMethods).map { HubMethodLabels.title($0) })
    }
}

#if DEBUG
@MainActor
private func provisioningPreview(only method: String? = nil) -> some View {
    let core = FakeCore()
    var config = AppSettings()
    if let method {
        config.hubMethods.directIp = method == "directIp"
        config.hubMethods.reality = method == "reality"
        config.hubMethods.shadowsocks = method == "shadowsocks"
        config.hubMethods.fronted = method == "fronted"
        config.hubMethods.normal = method == "normal"
    }
    core.configResult = .success(config)
    core.hubStatusValue = HubStatus(active: "reality", detail: nil)
    core.hubTestResults["fronted"] = .success(HubTestResult(method: "fronted", ok: false, ms: 0, unavailable: "noRelay"))
    return NavigationStack {
        ProvisioningPage()
    }
    .toastOverlay()
    .environment(SettingsStore(core: core))
    .environment(ToastCenter())
}

#Preview("Provisioning") {
    provisioningPreview()
}

#Preview("Provisioning, last method, dark") {
    provisioningPreview(only: "reality")
        .preferredColorScheme(.dark)
}
#endif
