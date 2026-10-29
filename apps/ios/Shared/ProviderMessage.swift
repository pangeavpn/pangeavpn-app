import Foundation

struct SwitchRequest: Codable, Equatable, Sendable {
    let serverId: String
    let candidates: [String]
}

struct SwitchReply: Codable, Equatable, Sendable {
    let accepted: Bool
}

enum ProviderMessage: UInt8 {
    case state = 1
    case switchServer = 2
    case logs = 3

    // A switch changes the tunnel, so the provider handles it; this answers the read-only kinds.
    static func reply(to message: Data, core: CoreBridge, reconnectDetail: String? = nil) throws -> Data? {
        guard let first = message.first, let kind = ProviderMessage(rawValue: first) else { return nil }
        switch kind {
        case .state:
            return try JSONEncoder().encode(status(core.state(), reconnectDetail: reconnectDetail))
        case .logs:
            return Data(core.logs().utf8)
        case .switchServer:
            return nil
        }
    }

    static func encodeSwitch(_ request: SwitchRequest) throws -> Data {
        Data([switchServer.rawValue]) + (try JSONEncoder().encode(request))
    }

    static func switchRequest(from message: Data) -> SwitchRequest? {
        guard message.first == switchServer.rawValue else { return nil }
        return try? JSONDecoder().decode(SwitchRequest.self, from: message.dropFirst())
    }

    // While Go is idle between attempts the user sees why, never Go's raw dial errors.
    static func status(_ status: TunnelStatus, reconnectDetail: String?) -> TunnelStatus {
        var shown = status
        if let reconnectDetail, status.state != .connected, status.state != .connecting {
            shown.detail = reconnectDetail
        } else if status.state == .error {
            shown.detail = TunnelStart.userMessage(coreText: status.detail)
        }
        return shown
    }
}
