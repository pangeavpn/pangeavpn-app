import Foundation
import os
import UserNotifications

// iOS shows these as the app's notifications, so they need the app's authorization.
// State lives on its own queue, so events can arrive from any queue.
final class ConnectionAlerts: @unchecked Sendable {
    private let queue = DispatchQueue(label: "org.pangeavpn.tunnel.alerts")
    private let log = Logger(subsystem: "org.pangeavpn", category: "alerts")
    private var policy = AlertPolicy()
    private var lostTimer: DispatchWorkItem?

    init() {}

    func handle(_ event: TunnelEvent) {
        queue.async {
            let actions = self.policy.decide(event, gate: ConnectionAlerts.currentGate())
            self.apply(actions)
        }
    }

    private static func currentGate() -> AlertPolicy.Gate {
        let defaults = AppGroup.defaults
        return AlertPolicy.Gate(
            enabled: SharedPrefs.connectionAlerts(in: defaults),
            appInForeground: SharedPrefs.appInForeground(in: defaults))
    }

    private func apply(_ actions: [AlertPolicy.Action]) {
        for action in actions {
            switch action {
            case .scheduleLost(let delay):
                scheduleLost(after: delay)
            case .cancelLost:
                lostTimer?.cancel()
                lostTimer = nil
            case .remove(let kinds):
                UNUserNotificationCenter.current()
                    .removeDeliveredNotifications(withIdentifiers: kinds.map(\.identifier))
            case .post(let alert):
                post(alert)
            }
        }
    }

    private func scheduleLost(after delay: TimeInterval) {
        lostTimer?.cancel()
        let timer = DispatchWorkItem { [weak self] in self?.lostDelayElapsed() }
        lostTimer = timer
        queue.asyncAfter(deadline: .now() + delay, execute: timer)
    }

    private func lostDelayElapsed() {
        lostTimer = nil
        apply(policy.lostDelayElapsed(gate: ConnectionAlerts.currentGate()))
    }

    private func post(_ alert: ConnectionAlert) {
        let text = alert.content
        let content = UNMutableNotificationContent()
        content.title = text.title
        content.body = text.body
        content.sound = text.playsSound ? UNNotificationSound.default : nil
        content.threadIdentifier = ConnectionAlert.threadIdentifier
        let kind = alert.kind.rawValue
        let request = UNNotificationRequest(identifier: alert.kind.identifier, content: content, trigger: nil)
        UNUserNotificationCenter.current().add(request) { [log] error in
            guard let error else { return log.info("posted \(kind, privacy: .public) alert") }
            let code = (error as NSError).code
            log.error("could not post \(kind, privacy: .public) alert, code \(code, privacy: .public)")
        }
    }
}
