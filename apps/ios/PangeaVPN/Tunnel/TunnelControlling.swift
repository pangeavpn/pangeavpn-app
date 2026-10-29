import Foundation

// What the saved VPN configuration enforces, separate from which server it reaches.
struct TunnelProfile: Equatable, Sendable {
    var strictKillSwitch: Bool
    var allowLan: Bool
    var autoConnect: Bool
    var lockdown: Bool

    static let standard = TunnelProfile(strictKillSwitch: false, allowLan: false, autoConnect: true, lockdown: false)
}

@MainActor
protocol TunnelControlling: AnyObject {
    var status: TunnelStatus { get }
    var connectedDate: Date? { get }
    // The saved Connect On Demand flag, which the user can also flip in iOS Settings.
    var onDemandActive: Bool { get }
    var onChange: (() -> Void)? { get set }

    func connect(serverId: String, candidates: [String], profile: TunnelProfile) async throws
    // Moves a running tunnel to another server without leaving the VPN session.
    func switchServer(serverId: String, candidates: [String]) async throws
    func updateProfile(_ profile: TunnelProfile) async
    // Turns Connect On Demand off and saves it before stopping, or iOS would restart the tunnel.
    func disconnect() async
    func removeConfiguration() async
    func extensionLogs() async -> String?
}
