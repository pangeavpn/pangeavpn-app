import Foundation

enum TunnelStartError: Int, LocalizedError {
    case noServer = 1
    case signedOut
    case noTunnelInterface

    var errorDescription: String? {
        switch self {
        case .noServer:
            return String(localized: "error_no_server", defaultValue: "Open PangeaVPN and pick a region.")
        case .signedOut:
            return String(localized: "error_signed_out", defaultValue: "Open PangeaVPN to sign in.")
        case .noTunnelInterface:
            return String(localized: "error_no_tun", defaultValue: "Could not open the tunnel interface")
        }
    }
}

enum TunnelStart {
    static let serverIdOption = "serverId"
    static let errorDomain = "org.pangeavpn.tunnel"
    static let otherErrorCode = 0

    private static let signedOutMarkers = ["no stored session", "no device identity"]

    static func resolveServerID(options: [String: NSObject]?, settings: AppSettings) throws -> String {
        if let chosen = options?[serverIdOption] as? String, !chosen.isEmpty {
            return chosen
        }
        guard !settings.lastServerId.isEmpty else { throw TunnelStartError.noServer }
        return settings.lastServerId
    }

    static func mapRestoreError(_ e: CoreError) -> Error {
        if signedOutMarkers.contains(where: { e.message.contains($0) }) {
            return TunnelStartError.signedOut
        }
        return e
    }

    // A Swift error's computed description can be lost crossing XPC to the app.
    static func providerError(_ error: Error) -> NSError {
        NSError(
            domain: errorDomain,
            code: (error as? TunnelStartError)?.rawValue ?? otherErrorCode,
            userInfo: [NSLocalizedDescriptionKey: error.localizedDescription])
    }
}
