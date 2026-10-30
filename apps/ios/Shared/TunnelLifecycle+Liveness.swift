import Foundation
import os

// Catches a tunnel that died quietly, as after a sleep on the same network with no path change.
extension TunnelLifecycle {
    func wake() {
        log.info("device woke")
        guard phase == .running else { return }
        liveness = liveness.armed(at: ticker.now, for: LivenessPolicy.wakeGrace)
        wakeTimers.forEach { $0.cancel() }
        let clock = ticker
        wakeTimers = LivenessPolicy.wakeChecks.map { delay in
            clock.after(delay) { [weak self] in self?.checkLiveness(reason: "wake") }
        }
    }

    func scheduleLivenessTick() {
        livenessTimer?.cancel()
        livenessTimer = ticker.after(LivenessPolicy.checkInterval) { [weak self] in self?.livenessTick() }
    }

    func checkLiveness(reason: String) {
        guard phase == .running else { return }
        let handshake = core.state().lastHandshake
        let wallNow = wallClock()
        let verdict = liveness.verdict(lastHandshake: handshake, wallNow: wallNow, now: ticker.now)
        guard verdict == .stale else {
            let shown = String(describing: verdict)
            return log.debug("liveness \(reason, privacy: .public): \(shown, privacy: .public)")
        }
        let age = Int(wallNow - TimeInterval(handshake))
        log.info("no handshake for \(age, privacy: .public)s (\(reason, privacy: .public)), reconnecting")
        beginRecovery(after: 0)
    }

    private func livenessTick() {
        livenessTimer = nil
        guard phase == .running else { return }
        logMemory("liveness check")
        checkLiveness(reason: "tick")
        guard phase == .running else { return }
        scheduleLivenessTick()
    }
}
