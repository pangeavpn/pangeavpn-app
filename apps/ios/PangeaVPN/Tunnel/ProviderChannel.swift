import Foundation
import NetworkExtension

enum TunnelSwitchError: LocalizedError, Equatable {
    case notRunning
    case rejected

    var errorDescription: String? {
        switch self {
        case .notRunning: return TunnelStartError.stopped.errorDescription
        case .rejected: return TunnelStartError.connectFailed.errorDescription
        }
    }
}

// App-to-extension messages, which NE only delivers while the provider process runs.
enum ProviderChannel {
    static let stateRequest = Data([ProviderMessage.state.rawValue])
    static let logsRequest = Data([ProviderMessage.logs.rawValue])

    static func canMessage(_ status: NEVPNStatus) -> Bool {
        switch status {
        case .connected, .connecting, .reasserting: return true
        case .invalid, .disconnected, .disconnecting: return false
        @unknown default: return false
        }
    }

    // An extension too old to know the switch message replies nil, which counts as a refusal.
    static func accepted(_ reply: Data?) -> Bool {
        guard let reply, let decoded = try? JSONDecoder().decode(SwitchReply.self, from: reply) else { return false }
        return decoded.accepted
    }

    static func text(_ reply: Data) -> String {
        String(decoding: reply, as: UTF8.self)
    }

    @MainActor
    static func send(_ message: Data, on session: NETunnelProviderSession) async throws -> Data? {
        try await withCheckedThrowingContinuation { continuation in
            do {
                try session.sendProviderMessage(message) { reply in
                    continuation.resume(returning: reply)
                }
            } catch {
                continuation.resume(throwing: error)
            }
        }
    }

    @MainActor
    static func lastDisconnectError(of connection: NEVPNConnection) async -> Error? {
        await withCheckedContinuation { continuation in
            connection.fetchLastDisconnectError { continuation.resume(returning: $0) }
        }
    }
}
