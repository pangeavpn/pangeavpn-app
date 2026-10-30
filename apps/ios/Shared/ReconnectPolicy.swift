import Foundation

struct ReconnectPolicy {
    enum Decision: Equatable {
        case ignore
        case reconnect(after: TimeInterval)
        case waitForNetwork
        case recheck(after: TimeInterval)
    }

    enum Failure: Equatable {
        case transient
        case locked
        case terminal(TunnelStartError)
    }

    let debounce: TimeInterval = 1.5
    // A path blip this close to our own settings apply is probably ours, so it is checked again later.
    let settleGrace: TimeInterval = 5
    let steppedBackoff: [TimeInterval] = [3, 6, 9]
    let steadyBackoff: TimeInterval = 30
    // Retrying a signed-out or expired account any sooner only hammers the hub.
    let terminalBackoff: TimeInterval = 300
    let lockedBackoff: TimeInterval = 5
    let rotateEvery = 3

    private static let terminalErrors: Set<TunnelStartError> = [
        .signedOut, .subscriptionExpired, .noSubscription, .noServer,
    ]

    private var lastKey: String?
    private var waiting = false
    private var appliedAt: TimeInterval?

    func backoff(attempt: Int) -> TimeInterval {
        guard attempt <= steppedBackoff.count else { return steadyBackoff }
        return steppedBackoff[max(attempt, 1) - 1]
    }

    func delay(after failure: Failure, attempt: Int) -> TimeInterval {
        switch failure {
        case .transient: return backoff(attempt: attempt)
        case .locked: return lockedBackoff
        case .terminal: return terminalBackoff
        }
    }

    func rotates(afterFailedAttempt attempt: Int) -> Bool {
        attempt > 0 && attempt % rotateEvery == 0
    }

    static func classify(_ error: Error) -> Failure {
        guard let mapped = TunnelStart.userFacing(error) as? TunnelStartError else { return .transient }
        if mapped == .deviceLocked { return .locked }
        return terminalErrors.contains(mapped) ? .terminal(mapped) : .transient
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

// The servers a session may use, best first; the current one is tried until rotated away from.
struct CandidateList: Equatable {
    let ids: [String]
    let index: Int

    init(_ ids: [String], index: Int = 0) {
        self.ids = ids
        self.index = ids.isEmpty ? 0 : min(max(index, 0), ids.count - 1)
    }

    var isEmpty: Bool { ids.isEmpty }

    var current: String { ids.isEmpty ? "" : ids[index] }

    func advanced() -> CandidateList {
        guard !ids.isEmpty else { return self }
        return CandidateList(ids, index: (index + 1) % ids.count)
    }

    // The new server leads and the one being left is the first fallback.
    static func forSwitch(to target: String, from previous: String, listed: [String]) -> CandidateList {
        var seen: Set<String> = []
        let ordered = ([target, previous] + listed).filter { !$0.isEmpty && seen.insert($0).inserted }
        return CandidateList(ordered)
    }
}
