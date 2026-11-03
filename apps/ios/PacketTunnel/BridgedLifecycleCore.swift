import Foundation

struct BridgedLifecycleCore: LifecycleCore {
    let bridge: CoreBridge

    func reload() throws { try bridge.reload() }

    func networkChanged() { bridge.networkChanged() }

    func keychainLocked() -> TunnelStartError? { KeychainProbe.lockedError() }

    func restore() throws -> [Server] { try bridge.restoreSession().servers }

    func config() throws -> AppSettings { try bridge.config() }

    func prepare(serverId: String) throws -> TunnelConfig { try bridge.prepare(serverId: serverId) }

    func prepareVia(exitId: String, entryId: String) throws -> TunnelConfig {
        try bridge.prepareVia(exitId: exitId, entryId: entryId)
    }

    func multihopEntries(exitId: String) -> [String] { bridge.multihopEntries(exitId: exitId) }

    func start(tunFd: Int) throws { try bridge.start(tunFd: tunFd) }

    func stop() { bridge.stop() }

    func stopForSwitch() { bridge.stopForSwitch() }

    func state() -> TunnelStatus { bridge.state() }
}
