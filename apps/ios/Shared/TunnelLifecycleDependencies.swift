import Foundation
import NetworkExtension

protocol LifecycleTimer: AnyObject {
    func cancel()
}

extension DispatchWorkItem: LifecycleTimer {}

protocol LifecycleScheduler: AnyObject {
    var now: TimeInterval { get }
    func after(_ delay: TimeInterval, _ work: @escaping () -> Void) -> LifecycleTimer
}

// Uptime pauses while the device sleeps, so timers and graces count only awake time.
final class DispatchLifecycleScheduler: LifecycleScheduler {
    private let queue: DispatchQueue

    init(queue: DispatchQueue) {
        self.queue = queue
    }

    var now: TimeInterval { ProcessInfo.processInfo.systemUptime }

    func after(_ delay: TimeInterval, _ work: @escaping () -> Void) -> LifecycleTimer {
        let item = DispatchWorkItem { work() }
        queue.asyncAfter(deadline: .now() + delay, execute: item)
        return item
    }
}

protocol LifecycleCore {
    func reload() throws
    func networkChanged()
    // Before the first unlock after a restart the session cannot be read at all.
    func keychainLocked() -> TunnelStartError?
    // The session's server list, which a start without one from the app falls back through.
    func restore() throws -> [Server]
    func config() throws -> AppSettings
    func prepare(serverId: String) throws -> TunnelConfig
    func prepareVia(exitId: String, entryId: String) throws -> TunnelConfig
    func multihopEntries(exitId: String) -> [String]
    func start(tunFd: Int) throws
    func stop()
    func stopForSwitch()
    func state() -> TunnelStatus
}

protocol LifecycleHost: AnyObject {
    func applyTunnelSettings(_ settings: NEPacketTunnelNetworkSettings, completion: @escaping (Error?) -> Void)
    func findTunFD() -> Int
    func markReasserting(_ value: Bool)
    func watchPath(_ enabled: Bool)
    func availableMemory() -> Int
}

struct PathUpdate: Equatable {
    let usable: Bool
    let key: String
}

// Read by app messages off the lifecycle queue, so it carries its own lock.
final class ReconnectNote: @unchecked Sendable {
    private let lock = NSLock()
    private var text: String?

    var value: String? { lock.withLock { text } }

    func set(_ value: String?) {
        lock.withLock { text = value }
    }
}
