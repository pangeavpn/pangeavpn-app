import Foundation

@MainActor
protocol TunnelControlling: AnyObject {
    var status: TunnelStatus { get }
    var connectedDate: Date? { get }
    var onChange: (() -> Void)? { get set }

    func connect(serverId: String, killSwitch: Bool, allowLan: Bool) async throws
    // Always releases the tunnel, even one NE already reports as disconnected.
    func disconnect() async
    func removeConfiguration() async
}
