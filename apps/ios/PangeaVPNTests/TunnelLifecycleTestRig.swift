import NetworkExtension
import XCTest
@testable import PangeaVPN

final class LifecycleRig {
    let core: LifecycleCoreFake
    let host: LifecycleHostFake
    let clock: LifecycleClock
    let ticker: LifecycleClock
    let wall: WallClockFake
    let note: ReconnectNote
    let prefs: UserDefaults
    let lifecycle: TunnelLifecycle
    private let recorder: EventRecorder
    private let suite: String
    private(set) var outcomes: [Int?] = []
    private(set) var messages: [String?] = []

    var events: [TunnelEvent] { recorder.events }

    init() {
        let suite = "lifecycle-tests-\(UUID().uuidString)"
        guard let prefs = UserDefaults(suiteName: suite) else { preconditionFailure("no test defaults suite") }
        let core = LifecycleCoreFake()
        let host = LifecycleHostFake()
        let clock = LifecycleClock()
        let ticker = LifecycleClock()
        let wall = WallClockFake()
        let note = ReconnectNote()
        let recorder = EventRecorder()
        core.settings.lastServerId = "last"
        self.suite = suite
        self.prefs = prefs
        self.core = core
        self.host = host
        self.clock = clock
        self.ticker = ticker
        self.wall = wall
        self.note = note
        self.recorder = recorder
        self.lifecycle = TunnelLifecycle(
            core: core, host: host, scheduler: clock, note: note, prefs: prefs, ticker: ticker,
            wallClock: { wall.now }, onEvent: { recorder.events.append($0) })
    }

    deinit {
        prefs.removePersistentDomain(forName: suite)
    }

    func start(options: [String: NSObject]? = [TunnelStart.serverIdOption: "srv" as NSString],
               excludeLocal: Bool? = nil) {
        lifecycle.start(options: options, excludeLocalNetworks: excludeLocal) { [weak self] error in
            let ns = error.map { $0 as NSError }
            self?.outcomes.append(ns?.code)
            self?.messages.append(ns?.localizedDescription)
        }
    }

    func start(server: String, candidates: [String]) {
        start(options: [
            TunnelStart.serverIdOption: server as NSString,
            TunnelStart.candidatesOption: TunnelStart.candidatesValue(candidates),
        ])
    }

    func path(_ key: String, usable: Bool = true) {
        lifecycle.pathChanged(PathUpdate(usable: usable, key: key))
    }

    func running(on key: String) -> LifecycleRig {
        start()
        return settled(on: key)
    }

    func settled(on key: String) -> LifecycleRig {
        path(key)
        core.clearCalls()
        host.clearHistory()
        recorder.events = []
        return self
    }

    func switchTo(_ serverId: String, candidates: [String] = []) {
        lifecycle.switchServer(SwitchRequest(serverId: serverId, candidates: candidates))
    }

    func handshake(agedBy seconds: TimeInterval) {
        core.status.lastHandshake = Int64(wall.now - seconds)
    }

    func useMultihop(entry: String?) {
        prefs.set(true, forKey: SharedPrefs.Key.multihopEnabled)
        if let entry { prefs.set(entry, forKey: SharedPrefs.Key.multihopEntryId) }
    }

    func useSplitTunnel(_ ranges: [String]) {
        prefs.set(true, forKey: SharedPrefs.Key.splitTunnelEnabled)
        prefs.set(ranges, forKey: SharedPrefs.Key.splitTunnelRanges)
    }

    func prepares() -> [String] {
        core.calls.filter { $0.hasPrefix("prepare") }
    }
}

final class EventRecorder {
    var events: [TunnelEvent] = []
}

final class WallClockFake {
    var now: TimeInterval = 1_800_000_000
}

final class LifecycleCoreFake: LifecycleCore {
    static let failure = CoreError(message: "no transport could establish a tunnel: cloak: dial 203.0.113.9")

    static func tunnelConfig(dns: [String], address: String = "10.66.0.2", mtu: Int = 1380) -> TunnelConfig {
        TunnelConfig(address: address, prefixLength: 32, dns: dns, mtu: mtu, serverId: "srv", serverName: "n")
    }

    var settings = AppSettings()
    var tunnel = LifecycleCoreFake.tunnelConfig(dns: ["10.66.0.1"])
    var restoreError: Error?
    var locked: TunnelStartError?
    var prepareErrors: [String: Error] = [:]
    var entries: [String] = []
    var status = TunnelStatus.disconnected
    var startFailures = 0
    var reloadFailures = 0
    private(set) var calls: [String] = []

    func reload() throws {
        calls.append("reload")
        guard reloadFailures > 0 else { return }
        reloadFailures -= 1
        throw CoreError(message: "decode stored config: unexpected end of JSON input")
    }

    func networkChanged() { calls.append("networkChanged") }

    func keychainLocked() -> TunnelStartError? { locked }

    func restore() throws {
        calls.append("restore")
        if let restoreError { throw restoreError }
    }

    func config() throws -> AppSettings {
        calls.append("config")
        return settings
    }

    func prepare(serverId: String) throws -> TunnelConfig {
        calls.append("prepare(\(serverId))")
        if let error = prepareErrors[serverId] { throw error }
        return tunnel
    }

    func prepareVia(exitId: String, entryId: String) throws -> TunnelConfig {
        calls.append("prepareVia(\(exitId),\(entryId))")
        if let error = prepareErrors[exitId] { throw error }
        return tunnel
    }

    func multihopEntries(exitId: String) -> [String] {
        calls.append("entries(\(exitId))")
        return entries
    }

    func start(tunFd: Int) throws {
        calls.append("start(\(tunFd))")
        guard startFailures > 0 else { return }
        startFailures -= 1
        throw Self.failure
    }

    func stop() { calls.append("stop") }

    func stopForSwitch() { calls.append("stopForSwitch") }

    func state() -> TunnelStatus { status }

    func clearCalls() { calls = [] }
}

final class LifecycleHostFake: LifecycleHost {
    var fd = 7
    var holdApply = false
    private(set) var reasserting = false
    private(set) var reassertingHistory: [Bool] = []
    private(set) var monitoring = false
    private(set) var applied: [NEPacketTunnelNetworkSettings] = []
    private(set) var memoryReads = 0
    private var held: [(Error?) -> Void] = []

    var applyCount: Int { applied.count }
    var lastSettings: NEPacketTunnelNetworkSettings? { applied.last }

    func applyTunnelSettings(_ settings: NEPacketTunnelNetworkSettings, completion: @escaping (Error?) -> Void) {
        applied.append(settings)
        if holdApply { held.append(completion) } else { completion(nil) }
    }

    // Releases only what is held now; an apply issued by a released callback stays held.
    func releaseApply(_ error: Error? = nil) {
        let pending = held
        held = []
        pending.forEach { $0(error) }
    }

    func findTunFD() -> Int { fd }

    func markReasserting(_ value: Bool) {
        reasserting = value
        if reassertingHistory.last != value { reassertingHistory.append(value) }
    }

    func watchPath(_ enabled: Bool) { monitoring = enabled }

    func availableMemory() -> Int {
        memoryReads += 1
        return 64 << 20
    }

    func clearHistory() {
        reassertingHistory = []
        applied = []
    }
}

final class LifecycleClock: LifecycleScheduler {
    final class Job: LifecycleTimer {
        let due: TimeInterval
        let work: () -> Void
        private(set) var cancelled = false

        init(due: TimeInterval, work: @escaping () -> Void) {
            self.due = due
            self.work = work
        }

        func cancel() { cancelled = true }
    }

    private(set) var now: TimeInterval = 0
    private var jobs: [Job] = []

    var pending: [TimeInterval] { jobs.filter { !$0.cancelled }.map { $0.due - now } }

    func after(_ delay: TimeInterval, _ work: @escaping () -> Void) -> LifecycleTimer {
        let job = Job(due: now + delay, work: work)
        jobs.append(job)
        return job
    }

    func advance(by delta: TimeInterval) {
        let end = now + delta
        while let next = jobs.filter({ !$0.cancelled && $0.due <= end }).min(by: { $0.due < $1.due }) {
            jobs.removeAll { $0 === next || $0.cancelled }
            now = next.due
            next.work()
        }
        jobs.removeAll { $0.cancelled }
        now = end
    }
}
