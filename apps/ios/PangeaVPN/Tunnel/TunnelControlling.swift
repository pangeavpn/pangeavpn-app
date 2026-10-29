import Foundation

@MainActor
protocol TunnelControlling: AnyObject {
    var status: TunnelStatus { get }
    var connectedDate: Date? { get }
    var onChange: (() -> Void)? { get set }

    func connect(serverId: String, killSwitch: Bool, allowLan: Bool) async throws
    func disconnect() async
}
