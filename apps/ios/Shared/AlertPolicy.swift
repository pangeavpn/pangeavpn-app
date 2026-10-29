import Foundation

enum ConnectionAlert: Equatable {
    case connected
    case lost
    case restored
    case blocked(TunnelStartError)

    // One identifier per kind, so a newer alert replaces the older one of its kind.
    enum Kind: String, CaseIterable {
        case connected, lost, restored, blocked

        var identifier: String { "org.pangeavpn.alert.\(rawValue)" }
    }

    static let threadIdentifier = "org.pangeavpn.connection"

    var kind: Kind {
        switch self {
        case .connected: return .connected
        case .lost: return .lost
        case .restored: return .restored
        case .blocked: return .blocked
        }
    }
}

struct AlertContent: Equatable {
    let title: String
    let body: String
    let playsSound: Bool
}

extension ConnectionAlert {
    var content: AlertContent {
        switch self {
        case .connected:
            return AlertContent(
                title: String(localized: "alerts_connected_title", defaultValue: "Connected"),
                body: String(localized: "alerts_connected_body", defaultValue: "Your traffic is now protected."),
                playsSound: false)
        case .lost:
            return AlertContent(
                title: String(localized: "alerts_lost_title", defaultValue: "Connection lost"),
                body: String(localized: "alerts_lost_body", defaultValue: "PangeaVPN is reconnecting…"),
                playsSound: false)
        case .restored:
            return AlertContent(
                title: String(localized: "alerts_restored_title", defaultValue: "Back online"),
                body: String(localized: "alerts_restored_body", defaultValue: "Your VPN connection is restored."),
                playsSound: false)
        case .blocked(let reason):
            return AlertContent(
                title: String(localized: "alerts_blocked_title", defaultValue: "Internet paused to protect you"),
                body: Self.blockedBody(reason),
                playsSound: true)
        }
    }

    private static func blockedBody(_ reason: TunnelStartError) -> String {
        let format = String(localized: "alerts_blocked_body", defaultValue: "%@ The kill switch is blocking traffic while PangeaVPN retries. Open the app and tap Disconnect to go online without VPN.")
        return String(format: format, reason.errorDescription ?? reason.localizedDescription)
    }
}

// Decides which alerts a tunnel event deserves; the caller does the posting and the timing.
struct AlertPolicy {
    struct Gate: Equatable {
        let enabled: Bool
        let appInForeground: Bool

        var allowsAlerts: Bool { enabled && !appInForeground }
    }

    enum Action: Equatable {
        case post(ConnectionAlert)
        case remove([ConnectionAlert.Kind])
        case scheduleLost(after: TimeInterval)
        case cancelLost
    }

    // Long enough that quick blips and in-place switches recover before anyone is told.
    static let lostDelay: TimeInterval = 10
    private static let outageKinds: [ConnectionAlert.Kind] = [.lost, .blocked]

    private var lostPending = false
    private var outageShown = false
    private var blockedShown = false

    // Removals ignore the gate: they only clear alerts that no longer describe the tunnel.
    mutating func decide(_ event: TunnelEvent, gate: Gate) -> [Action] {
        switch event {
        case .reconnecting: return beginRecovering()
        case .restored: return recover(gate: gate, otherwise: nil)
        // An in-app tap happens with the app open, so a start the gate lets through is on-demand.
        case .connected: return recover(gate: gate, otherwise: .connected)
        case .blocked(let reason): return block(reason, gate: gate)
        case .stopped: return end()
        }
    }

    // The gate is read when the delay ends, so opening the app meanwhile still suppresses it.
    mutating func lostDelayElapsed(gate: Gate) -> [Action] {
        guard lostPending else { return [] }
        lostPending = false
        guard gate.allowsAlerts else { return [] }
        outageShown = true
        return [.post(.lost)]
    }

    private mutating func beginRecovering() -> [Action] {
        guard !lostPending, !outageShown else { return [] }
        lostPending = true
        return [.scheduleLost(after: Self.lostDelay)]
    }

    private mutating func block(_ reason: TunnelStartError, gate: Gate) -> [Action] {
        let cancelled = cancelPendingLost()
        guard !blockedShown, gate.allowsAlerts else { return cancelled }
        blockedShown = true
        outageShown = true
        return cancelled + [.remove([.lost]), .post(.blocked(reason))]
    }

    // A start clears alerts a previous extension run may have left behind.
    private mutating func recover(gate: Gate, otherwise: ConnectionAlert?) -> [Action] {
        let announced = outageShown
        var actions = cancelPendingLost()
        reset()
        if announced || otherwise != nil { actions.append(.remove(Self.outageKinds)) }
        let alert: ConnectionAlert? = announced ? ConnectionAlert.restored : otherwise
        if gate.allowsAlerts, let alert { actions.append(.post(alert)) }
        return actions
    }

    private mutating func end() -> [Action] {
        let cancelled = cancelPendingLost()
        reset()
        return cancelled + [.remove(Self.outageKinds)]
    }

    private mutating func cancelPendingLost() -> [Action] {
        guard lostPending else { return [] }
        lostPending = false
        return [.cancelLost]
    }

    private mutating func reset() {
        lostPending = false
        outageShown = false
        blockedShown = false
    }
}
