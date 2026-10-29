import Foundation

struct BridgedLifecycleCore: LifecycleCore {
    let bridge: CoreBridge

    func reload() throws { try bridge.reload() }

    func networkChanged() { bridge.networkChanged() }

    func restore() throws { _ = try bridge.restoreSession() }

    func config() throws -> AppSettings { try bridge.config() }

    func prepare(serverId: String) throws -> TunnelConfig { try bridge.prepare(serverId: serverId) }

    func start(tunFd: Int) throws { try bridge.start(tunFd: tunFd) }

    func stop() { bridge.stop() }
}
