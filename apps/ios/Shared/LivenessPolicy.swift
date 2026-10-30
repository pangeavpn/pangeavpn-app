import Foundation

// Persistent keepalive is 25 s, so a live tunnel re-handshakes about every two minutes.
struct LivenessPolicy: Equatable {
    enum Verdict: Equatable {
        case unknown
        case grace
        case healthy
        case stale
    }

    static let checkInterval: TimeInterval = 15
    static let staleAfter: TimeInterval = 180
    static let grace: TimeInterval = 30
    // Traffic or the 25 s keepalive re-handshakes within seconds of a wake, so a dead tunnel is found fast.
    static let wakeGrace: TimeInterval = 15
    static let wakeChecks: [TimeInterval] = [17, 32]

    // On the monotonic clock: until then an old handshake is expected, not a fault.
    let graceUntil: TimeInterval?

    init(graceUntil: TimeInterval? = nil) {
        self.graceUntil = graceUntil
    }

    func armed(at now: TimeInterval, for grace: TimeInterval = LivenessPolicy.grace) -> LivenessPolicy {
        LivenessPolicy(graceUntil: now + grace)
    }

    // lastHandshake and wallNow are unix seconds; now is the clock the grace was armed on.
    func verdict(lastHandshake: Int64, wallNow: TimeInterval, now: TimeInterval) -> Verdict {
        guard lastHandshake > 0 else { return .unknown }
        if let graceUntil, now < graceUntil { return .grace }
        return wallNow - TimeInterval(lastHandshake) > Self.staleAfter ? .stale : .healthy
    }
}
