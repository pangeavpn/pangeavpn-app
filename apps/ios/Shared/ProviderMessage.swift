import Foundation

enum ProviderMessage: UInt8 {
    case state = 1

    static func reply(to message: Data, core: CoreBridge, reconnectDetail: String? = nil) throws -> Data? {
        guard let first = message.first, let kind = ProviderMessage(rawValue: first) else { return nil }
        switch kind {
        case .state:
            return try JSONEncoder().encode(status(core.state(), reconnectDetail: reconnectDetail))
        }
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
