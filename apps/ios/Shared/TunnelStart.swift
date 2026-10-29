import Foundation
import Security

enum TunnelStartError: Int, LocalizedError, CaseIterable {
    case noServer = 1
    case signedOut
    case noTunnelInterface
    case reconnectFailed
    case stopped
    case deviceLocked
    case subscriptionExpired
    case connectFailed
    case invalidDNS
    case settingsTimeout
    case noSubscription

    var errorDescription: String? {
        switch self {
        case .noServer:
            return String(localized: "error_no_server", defaultValue: "Open PangeaVPN and pick a region.")
        case .signedOut:
            return String(localized: "error_signed_out", defaultValue: "Open PangeaVPN to sign in.")
        case .noTunnelInterface, .settingsTimeout:
            return String(localized: "error_no_tun", defaultValue: "Could not open the tunnel interface")
        case .reconnectFailed:
            return String(
                localized: "error_reconnect_failed",
                defaultValue: "Could not reconnect after the network changed")
        case .stopped:
            return String(localized: "error_stopped", defaultValue: "The connection was stopped")
        case .deviceLocked:
            return String(localized: "error_device_locked", defaultValue: "Unlock your iPhone to connect.")
        case .subscriptionExpired:
            return String(localized: "error_subscription_expired", defaultValue: "Your subscription has expired.")
        case .noSubscription:
            return String(localized: "error_no_subscription", defaultValue: "This account has no active subscription.")
        case .connectFailed:
            return String(
                localized: "error_connect_failed",
                defaultValue: "Could not connect. Check your connection or try another region.")
        case .invalidDNS:
            return String(
                localized: "error_dns_invalid",
                defaultValue: "No usable DNS server. Check Custom DNS in Settings.")
        }
    }
}

enum TunnelStart {
    static let serverIdOption = "serverId"
    // Comma-joined server ids to fall back through, best first; the chosen server leads.
    static let candidatesOption = "candidates"
    static let errorDomain = "org.pangeavpn.tunnel"
    static let otherErrorCode = 0

    private static let signedOutMarkers = ["no stored session", "no device identity"]

    // First match wins, so the most specific markers come first.
    private static let coreMarkers: [(marker: String, error: TunnelStartError)] = [
        ("SUBSCRIPTION_EXPIRED", .subscriptionExpired),
        ("NO_SUBSCRIPTION", .noSubscription),
        ("unknown server", .noServer),
        ("no stored session", .signedOut),
        ("no device identity", .signedOut),
        ("not authenticated", .signedOut),
        ("hub auth error", .signedOut),
        ("DEVICE_NOT_REGISTERED", .signedOut),
    ]

    static func resolveServerID(options: [String: NSObject]?, settings: AppSettings) throws -> String {
        if let chosen = options?[serverIdOption] as? String, !chosen.isEmpty {
            return chosen
        }
        guard !settings.lastServerId.isEmpty else { throw TunnelStartError.noServer }
        return settings.lastServerId
    }

    // Always starts with the chosen server and never repeats one.
    static func resolveCandidates(options: [String: NSObject]?, serverId: String) -> [String] {
        let listed = (options?[candidatesOption] as? String)?
            .split(separator: ",").map { String($0).trimmingCharacters(in: .whitespaces) } ?? []
        var seen: Set<String> = [serverId]
        return [serverId] + listed.filter { !$0.isEmpty && seen.insert($0).inserted }
    }

    static func candidatesValue(_ ids: [String]) -> NSString {
        ids.joined(separator: ",") as NSString
    }

    // The app saves excludeLocalNetworks equal to Allow LAN, so routes then match the system flags.
    static func resolveAllowLan(excludeLocalNetworks: Bool?, settings: AppSettings) -> Bool {
        excludeLocalNetworks ?? settings.allowLan
    }

    static func keychainError(status: OSStatus) -> TunnelStartError? {
        status == errSecInteractionNotAllowed ? .deviceLocked : nil
    }

    static func mapRestoreError(_ e: CoreError) -> Error {
        if signedOutMarkers.contains(where: { e.message.contains($0) }) {
            return TunnelStartError.signedOut
        }
        return e
    }

    // Core text can carry dial errors and addresses, so the user only ever sees a mapped case.
    static func userFacing(_ error: Error) -> Error {
        guard let core = error as? CoreError else { return error }
        return classify(coreMessage: core.message)
    }

    static func classify(coreMessage: String) -> TunnelStartError {
        coreMarkers.first { coreMessage.contains($0.marker) }?.error ?? .connectFailed
    }

    static func userMessage(coreText: String) -> String {
        classify(coreMessage: coreText).localizedDescription
    }

    static func isStopped(_ error: Error) -> Bool {
        if let start = error as? TunnelStartError { return start == .stopped }
        let ns = error as NSError
        return ns.domain == errorDomain && ns.code == TunnelStartError.stopped.rawValue
    }

    // A Swift error's computed description can be lost crossing XPC to the app.
    static func providerError(_ error: Error) -> NSError {
        let ns = error as NSError
        if ns.domain == errorDomain { return ns }
        let shown = userFacing(error)
        return NSError(
            domain: errorDomain,
            code: (shown as? TunnelStartError)?.rawValue ?? otherErrorCode,
            userInfo: [NSLocalizedDescriptionKey: shown.localizedDescription])
    }
}
