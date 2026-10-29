#if DEBUG
import Foundation

@MainActor
final class FakeTunnel: TunnelControlling {
    struct Request: Equatable {
        let serverId: String
        let killSwitch: Bool
        let allowLan: Bool
    }

    var status = TunnelStatus.disconnected {
        didSet { onChange?() }
    }
    var connectedDate: Date?
    var onChange: (() -> Void)?
    var connectResult: Result<Void, CoreError> = .success(())

    private(set) var requests: [Request] = []
    private(set) var disconnects = 0
    private let onDisconnect: (() -> Void)?

    // onDisconnect lets a test log the disconnect into another fake's call order.
    init(onDisconnect: (() -> Void)? = nil) {
        self.onDisconnect = onDisconnect
    }

    func connect(serverId: String, killSwitch: Bool, allowLan: Bool) async throws {
        requests.append(Request(serverId: serverId, killSwitch: killSwitch, allowLan: allowLan))
        try connectResult.get()
        connectedDate = Date()
        status = TunnelStatus(
            state: .connected, detail: "", bytesIn: 0, bytesOut: 0,
            serverId: serverId, serverName: serverId, activeTransport: nil)
    }

    func disconnect() async {
        disconnects += 1
        onDisconnect?()
        connectedDate = nil
        status = .disconnected
    }
}
#endif
