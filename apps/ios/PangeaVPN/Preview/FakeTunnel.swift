#if DEBUG
import Foundation

@MainActor
final class FakeTunnel: TunnelControlling {
    struct Request: Equatable {
        let serverId: String
        let candidates: [String]
        let profile: TunnelProfile
    }

    struct Switch: Equatable {
        let serverId: String
        let candidates: [String]
    }

    var status = TunnelStatus.disconnected {
        didSet { onChange?() }
    }
    var connectedDate: Date?
    var onDemandActive = false
    var onChange: (() -> Void)?
    var connectResult: Result<Void, CoreError> = .success(())
    var switchResult: Result<Void, CoreError> = .success(())
    var logsValue: String?

    private(set) var requests: [Request] = []
    private(set) var switches: [Switch] = []
    private(set) var profiles: [TunnelProfile] = []
    private(set) var disconnects = 0
    private(set) var removals = 0
    private let onDisconnect: (() -> Void)?
    private let onRemove: (() -> Void)?

    // The hooks let a test log these calls into another fake's call order.
    init(onDisconnect: (() -> Void)? = nil, onRemove: (() -> Void)? = nil) {
        self.onDisconnect = onDisconnect
        self.onRemove = onRemove
    }

    func connect(serverId: String, candidates: [String], profile: TunnelProfile) async throws {
        requests.append(Request(serverId: serverId, candidates: candidates, profile: profile))
        try connectResult.get()
        connectedDate = Date()
        onDemandActive = profile.autoConnect || profile.lockdown
        status = Self.connected(to: serverId)
    }

    func switchServer(serverId: String, candidates: [String]) async throws {
        switches.append(Switch(serverId: serverId, candidates: candidates))
        try switchResult.get()
        status = Self.connected(to: serverId)
    }

    func updateProfile(_ profile: TunnelProfile) async {
        profiles.append(profile)
    }

    func disconnect() async {
        disconnects += 1
        onDisconnect?()
        onDemandActive = false
        connectedDate = nil
        status = .disconnected
    }

    func removeConfiguration() async {
        removals += 1
        onRemove?()
        onDemandActive = false
        connectedDate = nil
        status = .disconnected
    }

    func extensionLogs() async -> String? { logsValue }

    private static func connected(to serverId: String) -> TunnelStatus {
        TunnelStatus(
            state: .connected, detail: "", bytesIn: 0, bytesOut: 0,
            serverId: serverId, serverName: serverId, activeTransport: nil)
    }
}
#endif
