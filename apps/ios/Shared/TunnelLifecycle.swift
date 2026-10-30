import Foundation
import NetworkExtension
import os

struct Superseded: Error {}

// Every method must run on one serial queue, the same one both schedulers fire on.
final class TunnelLifecycle {
    enum Phase { case idle, starting, running, reconnecting, waiting, stopped }

    struct PendingStart {
        let cycle: Int
        let completion: (Error?) -> Void
    }

    let core: LifecycleCore
    weak var host: LifecycleHost?
    let scheduler: LifecycleScheduler
    let ticker: LifecycleScheduler
    let note: ReconnectNote
    let prefs: UserDefaults
    let settingsTimeout: TimeInterval
    let wallClock: () -> TimeInterval
    let onEvent: (TunnelEvent) -> Void
    let log = Logger(subsystem: "org.pangeavpn", category: "tunnel")

    var phase = Phase.idle
    var policy = ReconnectPolicy()
    var liveness = LivenessPolicy()
    var targets = CandidateList([])
    var startOptions: [String: NSObject]?
    var excludeLocalNetworks: Bool?
    var allowLan = false
    var systemStart = false
    var sessionReady = false
    var blockingDone = false
    var preparedExit: String?
    var appliedFingerprint: TunnelSettingsFingerprint?
    var cycle = 0
    var session = 0
    var pendingStart: PendingStart?
    var attemptTimer: LifecycleTimer?
    var recheckTimer: LifecycleTimer?
    var livenessTimer: LifecycleTimer?
    var wakeTimers: [LifecycleTimer] = []
    var latest: PathUpdate?
    var startBaseline: String?
    var everConnected = false
    var outage = false

    var serverId: String { targets.current }

    init(core: LifecycleCore, host: LifecycleHost, scheduler: LifecycleScheduler,
         note: ReconnectNote = ReconnectNote(), settingsTimeout: TimeInterval = 5,
         prefs: UserDefaults = AppGroup.defaults, ticker: LifecycleScheduler? = nil,
         wallClock: @escaping () -> TimeInterval = { Date().timeIntervalSince1970 },
         onEvent: @escaping (TunnelEvent) -> Void = { _ in }) {
        self.core = core
        self.host = host
        self.scheduler = scheduler
        self.ticker = ticker ?? scheduler
        self.note = note
        self.prefs = prefs
        self.settingsTimeout = settingsTimeout
        self.wallClock = wallClock
        self.onEvent = onEvent
    }

    // Blocking routes go up before anything reaches the hub, so nothing leaks while it is reached.
    func start(options: [String: NSObject]?, excludeLocalNetworks: Bool?, completion: @escaping (Error?) -> Void) {
        resetSession()
        phase = .starting
        startOptions = options
        // The app always names a server; iOS may pass options of its own on an on-demand start.
        systemStart = options?[TunnelStart.serverIdOption] == nil
        self.excludeLocalNetworks = excludeLocalNetworks
        allowLan = excludeLocalNetworks ?? false
        pendingStart = PendingStart(cycle: cycle, completion: completion)
        host?.watchPath(true)
        logMemory("start")
        applyBlocking(session: session)
    }

    func stop() {
        let announce = phase != .stopped
        phase = .stopped
        cycle += 1
        cancelTimers()
        host?.watchPath(false)
        core.stop()
        note.set(nil)
        abandonPendingStart()
        everConnected = false
        outage = false
        if announce { onEvent(.stopped) }
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

    func blockingApplied(_ error: Error?, session started: Int) {
        guard started == session, phase == .starting, let pending = pendingStart else { return }
        if let error { return finishStart(error, cycle: pending.cycle) }
        blockingDone = true
        runStartSequence(cycle: pending.cycle)
    }

    func runStartSequence(cycle started: Int) {
        reloadCore()
        do {
            try ensureSession()
        } catch {
            return finishStart(error, cycle: started)
        }
        tryCandidate(at: 0, cycle: started)
    }

    func finishStart(_ error: Error?, cycle started: Int) {
        guard let pending = pendingStart, pending.cycle == started else { return }
        pendingStart = nil
        if let error, !continuesBlocked(after: error) { return failStart(error, pending) }
        if let startBaseline { policy.seed(key: startBaseline) }
        pending.completion(nil)
        if let error {
            log.info("system start stays up with traffic blocked and keeps retrying")
            phase = .reconnecting
            retry(afterFailedAttempt: 1, error: error)
        } else {
            log.info("tunnel started")
            cameUp()
        }
        if let latest { handle(latest) }
    }

    // Shared by a start, a reconnect and a switch that succeeded.
    func cameUp() {
        phase = .running
        note.set(nil)
        host?.markReasserting(false)
        liveness = liveness.armed(at: ticker.now)
        scheduleLivenessTick()
        announceUp()
    }

    func announceUp() {
        if !everConnected {
            onEvent(.connected)
        } else if outage {
            onEvent(.restored)
        }
        everConnected = true
        outage = false
    }

    // Every terminal failure is told, so an alert the open app held back can show once it closes.
    func announceTrouble(_ failure: ReconnectPolicy.Failure) {
        switch failure {
        case .terminal(let reason):
            outage = true
            onEvent(.blocked(reason))
        case .transient:
            guard !outage else { return }
            outage = true
            onEvent(.reconnecting)
        case .locked:
            return
        }
    }

    func logMemory(_ moment: String) {
        let bytes = host?.availableMemory() ?? 0
        log.debug("available memory at \(moment, privacy: .public): \(bytes, privacy: .public) bytes")
    }

    // A decode failure leaves Go on Init's defaults, and connecting on those beats failing.
    func reloadCore() {
        do {
            try core.reload()
        } catch {
            log.error("reload failed, continuing: \(error.localizedDescription, privacy: .private)")
        }
    }

    func cancelTimers() {
        attemptTimer?.cancel()
        attemptTimer = nil
        recheckTimer?.cancel()
        recheckTimer = nil
        livenessTimer?.cancel()
        livenessTimer = nil
        wakeTimers.forEach { $0.cancel() }
        wakeTimers = []
    }

    // Raw core text can carry hub and node addresses, so only the mapped code is public.
    func logFailure(_ step: String, _ error: Error) {
        if TunnelStart.isStopped(error) { return log.info("\(step, privacy: .public) abandoned: stop requested") }
        let code = TunnelStart.providerError(error).code
        let detail = error.localizedDescription
        log.error("\(step, privacy: .public) failed, code \(code, privacy: .public): \(detail, privacy: .private)")
    }

    private func tryCandidate(at index: Int, cycle started: Int) {
        targets = CandidateList(targets.ids, index: index)
        establish(cycle: started, switching: false) { [weak self] error in
            self?.candidateFinished(at: index, error: error, cycle: started)
        }
    }

    private func candidateFinished(at index: Int, error: Error?, cycle started: Int) {
        guard started == cycle, pendingStart?.cycle == started else { return }
        guard let error else { return finishStart(nil, cycle: started) }
        let next = index + 1
        guard next < targets.ids.count, Self.worthNextCandidate(after: error) else {
            return finishStart(error, cycle: started)
        }
        logFailure("start on candidate \(index + 1)", error)
        tryCandidate(at: next, cycle: started)
    }

    // A server the hub no longer lists is skipped; an account problem would fail everywhere.
    static func worthNextCandidate(after error: Error) -> Bool {
        if TunnelStart.isStopped(error) { return false }
        switch ReconnectPolicy.classify(error) {
        case .transient, .terminal(.noServer): return true
        case .locked, .terminal: return false
        }
    }

    // On-demand and Settings starts have no app to show an error, so they stay up and blocked.
    private func continuesBlocked(after error: Error) -> Bool {
        systemStart && blockingDone && !TunnelStart.isStopped(error)
    }

    private func failStart(_ error: Error, _ pending: PendingStart) {
        phase = .idle
        cancelTimers()
        host?.watchPath(false)
        logFailure("start", error)
        pending.completion(TunnelStart.providerError(error))
    }

    private func resetSession() {
        abandonPendingStart()
        cycle += 1
        session += 1
        cancelTimers()
        policy = ReconnectPolicy()
        liveness = LivenessPolicy()
        targets = CandidateList([])
        startOptions = nil
        allowLan = false
        sessionReady = false
        blockingDone = false
        preparedExit = nil
        appliedFingerprint = nil
        latest = nil
        startBaseline = nil
        everConnected = false
        outage = false
        note.set(nil)
    }

    private func abandonPendingStart() {
        guard let pending = pendingStart else { return }
        pendingStart = nil
        log.info("start abandoned: stop requested")
        pending.completion(TunnelStart.providerError(TunnelStartError.stopped))
    }
}
