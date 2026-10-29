import Foundation
import NetworkExtension
import os

struct ReconnectPolicy {
    enum Decision: Equatable {
        case ignore
        case reconnect(after: TimeInterval)
        case waitForNetwork
        case recheck(after: TimeInterval)
    }

    let debounce: TimeInterval = 1.5
    // A path blip this close to our own settings apply is probably ours, so it is checked again later.
    let settleGrace: TimeInterval = 5
    let steppedBackoff: [TimeInterval] = [3, 6, 9]
    let steadyBackoff: TimeInterval = 30

    private var lastKey: String?
    private var waiting = false
    private var appliedAt: TimeInterval?

    func backoff(attempt: Int) -> TimeInterval {
        guard attempt <= steppedBackoff.count else { return steadyBackoff }
        return steppedBackoff[max(attempt, 1) - 1]
    }

    mutating func seed(key: String) {
        guard !key.isEmpty else { return }
        lastKey = key
    }

    mutating func settingsApplied(at time: TimeInterval) {
        appliedAt = time
    }

    mutating func markWaiting() {
        waiting = true
    }

    mutating func pathChanged(key: String, usable: Bool, now: TimeInterval) -> Decision {
        guard usable else {
            if let left = graceLeft(now: now) { return .recheck(after: left) }
            waiting = true
            return .waitForNetwork
        }
        guard !key.isEmpty else { return .ignore }
        let previous = lastKey
        lastKey = key
        if waiting {
            waiting = false
            return .reconnect(after: debounce)
        }
        guard let previous, previous != key else { return .ignore }
        return .reconnect(after: debounce)
    }

    private func graceLeft(now: TimeInterval) -> TimeInterval? {
        guard let appliedAt else { return nil }
        let left = appliedAt + settleGrace - now
        return left > 0 ? left : nil
    }
}

protocol LifecycleTimer: AnyObject {
    func cancel()
}

extension DispatchWorkItem: LifecycleTimer {}

protocol LifecycleScheduler: AnyObject {
    var now: TimeInterval { get }
    func after(_ delay: TimeInterval, _ work: @escaping () -> Void) -> LifecycleTimer
}

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
    func restore() throws
    func config() throws -> AppSettings
    func prepare(serverId: String) throws -> TunnelConfig
    func start(tunFd: Int) throws
    func stop()
}

protocol LifecycleHost: AnyObject {
    func applyTunnelSettings(_ settings: NEPacketTunnelNetworkSettings, completion: @escaping (Error?) -> Void)
    func findTunFD() -> Int
    func markReasserting(_ value: Bool)
    func watchPath(_ enabled: Bool)
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

private struct Superseded: Error {}

// Every method must run on one serial queue, the same one the scheduler fires on.
final class TunnelLifecycle {
    private enum Phase { case idle, starting, running, reconnecting, waiting, stopped }

    private struct PendingStart {
        let cycle: Int
        let completion: (Error?) -> Void
    }

    private let core: LifecycleCore
    private weak var host: LifecycleHost?
    private let scheduler: LifecycleScheduler
    private let note: ReconnectNote
    private let settingsTimeout: TimeInterval
    private let log = Logger(subsystem: "org.pangeavpn", category: "tunnel")

    private var phase = Phase.idle
    private var policy = ReconnectPolicy()
    private var serverId = ""
    private var allowLan = false
    private var cycle = 0
    private var pendingStart: PendingStart?
    private var attemptTimer: LifecycleTimer?
    private var recheckTimer: LifecycleTimer?
    private var latest: PathUpdate?
    private var startBaseline: String?

    init(core: LifecycleCore, host: LifecycleHost, scheduler: LifecycleScheduler,
         note: ReconnectNote = ReconnectNote(), settingsTimeout: TimeInterval = 5) {
        self.core = core
        self.host = host
        self.scheduler = scheduler
        self.note = note
        self.settingsTimeout = settingsTimeout
    }

    func start(options: [String: NSObject]?, excludeLocalNetworks: Bool?, completion: @escaping (Error?) -> Void) {
        resetSession()
        phase = .starting
        let started = cycle
        pendingStart = PendingStart(cycle: started, completion: completion)
        host?.watchPath(true)
        reloadCore()
        do {
            let settings = try restoreSessionAndConfig()
            serverId = try TunnelStart.resolveServerID(options: options, settings: settings)
            allowLan = TunnelStart.resolveAllowLan(excludeLocalNetworks: excludeLocalNetworks, settings: settings)
            establish(cycle: started) { [weak self] error in self?.finishStart(error, cycle: started) }
        } catch {
            finishStart(error, cycle: started)
        }
    }

    func stop() {
        phase = .stopped
        cycle += 1
        cancelTimers()
        host?.watchPath(false)
        core.stop()
        note.set(nil)
        abandonPendingStart()
    }

    func pathChanged(_ update: PathUpdate) {
        latest = update
        switch phase {
        case .starting:
            if startBaseline == nil, update.usable, !update.key.isEmpty { startBaseline = update.key }
        case .running, .reconnecting, .waiting:
            handle(update)
        case .idle, .stopped:
            break
        }
    }

    private func resetSession() {
        abandonPendingStart()
        cycle += 1
        cancelTimers()
        policy = ReconnectPolicy()
        serverId = ""
        allowLan = false
        latest = nil
        startBaseline = nil
        note.set(nil)
    }

    private func abandonPendingStart() {
        guard let pending = pendingStart else { return }
        pendingStart = nil
        log.info("start abandoned: stop requested")
        pending.completion(TunnelStart.providerError(TunnelStartError.stopped))
    }

    private func restoreSessionAndConfig() throws -> AppSettings {
        do {
            try core.restore()
        } catch let error as CoreError {
            throw TunnelStart.mapRestoreError(error)
        }
        return try core.config()
    }

    private func finishStart(_ error: Error?, cycle started: Int) {
        guard let pending = pendingStart, pending.cycle == started else { return }
        pendingStart = nil
        guard let error else {
            phase = .running
            if let startBaseline { policy.seed(key: startBaseline) }
            log.info("tunnel started")
            pending.completion(nil)
            if let latest { handle(latest) }
            return
        }
        phase = .idle
        host?.watchPath(false)
        logFailure("start", error)
        pending.completion(TunnelStart.providerError(error))
    }

    private func handle(_ update: PathUpdate) {
        switch policy.pathChanged(key: update.key, usable: update.usable, now: scheduler.now) {
        case .ignore:
            return
        case .recheck(let delay):
            recheckTimer?.cancel()
            recheckTimer = scheduler.after(delay) { [weak self] in self?.recheck() }
        case .waitForNetwork:
            enterWaiting()
        case .reconnect(let delay):
            log.info("network changed, reconnecting in \(delay, privacy: .public)s")
            beginCycle(after: delay)
        }
    }

    private func recheck() {
        recheckTimer = nil
        guard phase == .running || phase == .reconnecting || phase == .waiting, let latest else { return }
        handle(latest)
    }

    // Go stays stopped and the routes stay applied, so traffic fails closed until the network returns.
    private func enterWaiting() {
        policy.markWaiting()
        guard phase != .waiting else { return }
        phase = .waiting
        cycle += 1
        cancelTimers()
        host?.markReasserting(true)
        core.stop()
        log.info("network unavailable, waiting")
    }

    private func beginCycle(after delay: TimeInterval) {
        phase = .reconnecting
        cycle += 1
        cancelTimers()
        scheduleAttempt(1, after: delay)
    }

    private func scheduleAttempt(_ attempt: Int, after delay: TimeInterval) {
        let current = cycle
        attemptTimer = scheduler.after(delay) { [weak self] in self?.runAttempt(attempt, cycle: current) }
    }

    private func runAttempt(_ attempt: Int, cycle current: Int) {
        guard phase == .reconnecting, current == cycle else { return }
        attemptTimer = nil
        host?.markReasserting(true)
        if latest?.usable == false { return enterWaiting() }
        if attempt == 1 { core.networkChanged() }
        reloadCore()
        if attempt == 1, restartInPlace() { return attemptFinished(attempt, cycle: current, error: nil) }
        core.stop()
        establish(cycle: current) { [weak self] error in
            self?.attemptFinished(attempt, cycle: current, error: error)
        }
    }

    // A decode failure leaves Go on Init's defaults, and connecting on those beats failing.
    private func reloadCore() {
        do {
            try core.reload()
        } catch {
            log.error("reload failed, continuing: \(error.localizedDescription, privacy: .private)")
        }
    }

    // Go keeps the prepared config across stop, so this skips the hub and the settings apply.
    private func restartInPlace() -> Bool {
        core.stop()
        let fd = host?.findTunFD() ?? -1
        guard fd >= 0 else {
            log.info("tunnel interface not found, preparing again")
            return false
        }
        do {
            try core.start(tunFd: fd)
            return true
        } catch {
            logFailure("restart in place", error)
            return false
        }
    }

    private func attemptFinished(_ attempt: Int, cycle current: Int, error: Error?) {
        guard phase == .reconnecting, current == cycle else { return }
        guard let error else {
            phase = .running
            note.set(nil)
            host?.markReasserting(false)
            return log.info("reconnected on attempt \(attempt, privacy: .public)")
        }
        logFailure("reconnect attempt \(attempt)", error)
        note.set(TunnelStart.providerError(error).localizedDescription)
        if latest?.usable == false { return enterWaiting() }
        scheduleAttempt(attempt + 1, after: policy.backoff(attempt: attempt))
    }

    private func establish(cycle current: Int, _ completion: @escaping (Error?) -> Void) {
        do {
            let tunnel = try core.prepare(serverId: serverId)
            let settings = try TunnelSettingsBuilder.build(tunnel, allowLan: allowLan)
            applySettings(settings) { [weak self] error in
                self?.startCore(settingsError: error, cycle: current, completion: completion)
            }
        } catch {
            completion(error)
        }
    }

    private func applySettings(_ settings: NEPacketTunnelNetworkSettings, completion: @escaping (Error?) -> Void) {
        guard let host else { return completion(TunnelStartError.stopped) }
        policy.settingsApplied(at: scheduler.now)
        SettingsApply.run(
            timeout: settingsTimeout,
            scheduler: scheduler,
            apply: { done in host.applyTunnelSettings(settings, completion: done) },
            completion: { [weak self] error in
                guard let self else { return completion(error) }
                self.policy.settingsApplied(at: self.scheduler.now)
                completion(self.proceedingPastTimeout(error))
            })
    }

    // As in WireGuardKit, a missing callback is not a failure: the fd probe decides instead.
    private func proceedingPastTimeout(_ error: Error?) -> Error? {
        guard let timeout = error as? TunnelStartError, timeout == .settingsTimeout else { return error }
        log.error("settings apply did not call back, proceeding anyway")
        return nil
    }

    // The utun only exists once the network settings have been applied.
    private func startCore(settingsError: Error?, cycle current: Int, completion: (Error?) -> Void) {
        if let settingsError { return completion(settingsError) }
        guard phase != .stopped else { return completion(TunnelStartError.stopped) }
        guard current == cycle else { return completion(Superseded()) }
        let fd = host?.findTunFD() ?? -1
        guard fd >= 0 else { return completion(TunnelStartError.noTunnelInterface) }
        do {
            try core.start(tunFd: fd)
            completion(nil)
        } catch {
            completion(error)
        }
    }

    private func cancelTimers() {
        attemptTimer?.cancel()
        attemptTimer = nil
        recheckTimer?.cancel()
        recheckTimer = nil
    }

    // Raw core text can carry hub and node addresses, so only the mapped code is public.
    private func logFailure(_ step: String, _ error: Error) {
        if TunnelStart.isStopped(error) { return log.info("\(step, privacy: .public) abandoned: stop requested") }
        let code = TunnelStart.providerError(error).code
        let detail = error.localizedDescription
        log.error("\(step, privacy: .public) failed, code \(code, privacy: .public): \(detail, privacy: .private)")
    }
}
