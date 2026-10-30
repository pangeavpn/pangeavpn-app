import SwiftUI
import UIKit
import UserNotifications

struct NotificationsPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.openURL) private var openURL
    @Environment(\.scenePhase) private var scenePhase
    @State private var permission = NotificationPermission()

    init() {}

    var body: some View {
        SettingsPage(.notifications, description: String(localized: "notifications_description", defaultValue: "System notifications about your connection.")) {
            SettingsCard {
                StoreSwitchRow(
                    title: Self.alertsTitle,
                    value: settings.connectionAlerts,
                    hint: String(localized: "notifications_alerts_hint", defaultValue: "Tells you when the VPN drops, comes back, or is blocking traffic."),
                    apply: { await setAlerts($0) }
                )
                if settings.connectionAlerts {
                    permissionState
                }
            }
        }
        .task { await permission.refresh() }
        .onChange(of: scenePhase) { _, phase in
            // The user may have changed the permission in iOS Settings meanwhile.
            if phase == .active { Task { await permission.refresh() } }
        }
    }

    static var alertsTitle: String {
        String(localized: "notifications_alerts_title", defaultValue: "Connection alerts")
    }

    @ViewBuilder
    private var permissionState: some View {
        switch permission.status {
        case .denied:
            InfoNote(String(localized: "notifications_denied", defaultValue: "Notifications for PangeaVPN are off in iOS Settings, so alerts can't be shown."), systemImage: "bell.slash", tint: Palette.error)
            GhostButton(String(localized: "notifications_open_settings", defaultValue: "Open Settings")) { openSettings() }
        case .notDetermined:
            InfoNote(String(localized: "notifications_not_asked", defaultValue: "PangeaVPN needs your permission to show alerts."), systemImage: "bell.badge")
            GhostButton(String(localized: "notifications_allow", defaultValue: "Allow Notifications")) {
                Task { await askPermission() }
            }
        default:
            EmptyView()
        }
    }

    private func setAlerts(_ enabled: Bool) async {
        guard enabled != settings.connectionAlerts else { return }
        settings.setConnectionAlerts(enabled)
        guard enabled else {
            toasts.show(String(localized: "notifications_toast_off", defaultValue: "Connection alerts off."), style: .info)
            return
        }
        await askPermission()
    }

    // A refusal leaves the preference on; the page then shows how to allow alerts in iOS Settings.
    private func askPermission() async {
        if await permission.request() {
            toasts.show(String(localized: "notifications_toast_on", defaultValue: "Connection alerts on."), style: .success)
        }
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        openURL(url)
    }
}

extension SettingsSummary {
    static func notifications(_ settings: SettingsStore) -> String {
        settings.connectionAlerts ? NotificationsPage.alertsTitle : off()
    }
}

#if DEBUG
#Preview("Notifications") {
    ProtectionPreview.stage(NotificationsPage())
}

#Preview("Notifications off, dark") {
    ProtectionPreview.stage(NotificationsPage()) { $0.setConnectionAlerts(false) }
        .preferredColorScheme(.dark)
}
#endif
