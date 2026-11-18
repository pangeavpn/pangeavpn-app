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
        guard let due = WakeRekey.delay(lastHandshake: core.state().lastHandshake, wallNow: wallClock()) else { return }
        if due == 0 { return renewKeys() }
        wakeTimers.append(clock.after(due) { [weak self] in self?.renewKeys() })
    }

    // A fresh handshake keeps the session; only a carrier that stays silent costs a reconnect.
    private func renewKeys() {
        guard phase == .running else { return }
        let before = core.state().lastHandshake
        log.info("renewing the session keys after sleep")
        core.rekey()
        wakeTimers.append(ticker.after(WakeRekey.confirmWithin) { [weak self] in self?.confirmRenewal(since: before) })
    }

    private func confirmRenewal(since before: Int64) {
        guard phase == .running, core.state().lastHandshake <= before else { return }
        log.info("no handshake after renewing the keys, reconnecting")
        beginRecovery(after: 0)
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
