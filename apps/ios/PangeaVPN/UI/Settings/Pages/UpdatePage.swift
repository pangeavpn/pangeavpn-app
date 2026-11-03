import SwiftUI

enum UpdateText {
    // App Review runs builds with the same sandbox receipt as TestFlight, so the page never names TestFlight.
    static var explanation: String {
        String(localized: "update_appstore", defaultValue: "PangeaVPN updates through the App Store. Turn on automatic updates in Settings › App Store.")
    }
}

struct AppBuildInfo: Equatable, Sendable {
    let version: String
    let build: String

    init(info: [String: Any]?) {
        version = Self.text(info?["CFBundleShortVersionString"]) ?? "0.0.0"
        build = Self.text(info?["CFBundleVersion"]) ?? "0"
    }

    static var current: AppBuildInfo {
        AppBuildInfo(info: Bundle.main.infoDictionary)
    }

    private static func text(_ value: Any?) -> String? {
        guard let string = value as? String, !string.isEmpty else { return nil }
        return string
    }
}

struct UpdatePage: View {
    private let info = AppBuildInfo.current

    init() {}

    var body: some View {
        SettingsPage(.update, description: UpdateText.explanation) {
            SettingsCard(String(localized: "update_installed_heading", defaultValue: "Installed version")) {
                VStack(spacing: 0) {
                    VersionDetailRow(label: String(localized: "update_version", defaultValue: "Version"), value: info.version)
                    Divider().overlay(Palette.borderSubtle)
                    VersionDetailRow(label: String(localized: "update_build", defaultValue: "Build"), value: info.build)
                }
                .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
            }
        }
    }
}

private struct VersionDetailRow: View {
    let label: String
    let value: String

    var body: some View {
        HStack(spacing: 12) {
            Text(label)
                .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                .foregroundStyle(Palette.text)
            Spacer(minLength: 8)
            Text(value)
                .scaledFont(size: 14, design: .monospaced, relativeTo: .subheadline)
                .foregroundStyle(Palette.textSecondary)
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 48)
        .accessibilityElement(children: .combine)
    }
}

extension SettingsSummary {
    static func update() -> String {
        "v" + AppBuildInfo.current.version
    }
}

#if DEBUG
#Preview("Software update") {
    NavigationStack {
        UpdatePage()
    }
    .environment(SettingsStore(core: FakeCore()))
    .environment(ToastCenter())
}
#endif
