import SwiftUI

enum UpdateChannel: Equatable, Sendable {
    case appStore, testFlight

    // TestFlight installs carry a sandbox receipt; App Store installs carry a production one.
    init(receiptURL: URL?) {
        self = receiptURL?.lastPathComponent == "sandboxReceipt" ? .testFlight : .appStore
    }

    var explanation: String {
        switch self {
        case .appStore: return String(localized: "update_appstore", defaultValue: "PangeaVPN updates through the App Store. Turn on automatic updates in Settings › App Store.")
        case .testFlight: return String(localized: "update_testflight", defaultValue: "PangeaVPN updates through TestFlight. Turn on automatic updates for PangeaVPN in the TestFlight app.")
        }
    }
}

struct AppBuildInfo: Equatable, Sendable {
    let version: String
    let build: String
    let channel: UpdateChannel

    init(info: [String: Any]?, receiptURL: URL?) {
        version = Self.text(info?["CFBundleShortVersionString"]) ?? "0.0.0"
        build = Self.text(info?["CFBundleVersion"]) ?? "0"
        channel = UpdateChannel(receiptURL: receiptURL)
    }

    static var current: AppBuildInfo {
        AppBuildInfo(info: Bundle.main.infoDictionary, receiptURL: Bundle.main.appStoreReceiptURL)
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
        SettingsPage(.update, description: info.channel.explanation) {
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
