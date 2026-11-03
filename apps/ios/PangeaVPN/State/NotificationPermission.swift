import Foundation
import Observation
import os
import UserNotifications

protocol NotificationAuthorizing: Sendable {
    func authorizationStatus() async -> UNAuthorizationStatus
    func requestAuthorization() async throws -> Bool
}

// The tunnel extension posts connection alerts under the app's authorization.
struct SystemNotificationAuthorizer: NotificationAuthorizing {
    func authorizationStatus() async -> UNAuthorizationStatus {
        await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
    }

    func requestAuthorization() async throws -> Bool {
        try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
    }
}

@MainActor
@Observable
final class NotificationPermission {
    var status: UNAuthorizationStatus = .notDetermined

    private let center: any NotificationAuthorizing
    private static let log = Logger(subsystem: "org.pangeavpn", category: "notifications")

    init(center: any NotificationAuthorizing = SystemNotificationAuthorizer()) {
        self.center = center
    }

    func refresh() async {
        status = await center.authorizationStatus()
    }

    // After a denial iOS stops asking and answers false, so the page must point to Settings.
    func request() async -> Bool {
        let granted: Bool
        do {
            granted = try await center.requestAuthorization()
        } catch {
            Self.log.error("notification permission request failed: \(error.localizedDescription, privacy: .public)")
            granted = false
        }
        await refresh()
        return granted
    }
}
