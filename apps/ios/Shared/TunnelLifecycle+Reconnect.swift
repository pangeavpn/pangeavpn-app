import Foundation
import os

extension TunnelLifecycle {
    func handle(_ update: PathUpdate) {
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
            beginRecovery(after: delay)
        }
    }

    func beginRecovery(after delay: TimeInterval) {
        announceTrouble(.transient)
        phase = .reconnecting
        cycle += 1
        cancelTimers()
        scheduleAttempt(1, after: delay)
    }

    // Go stays stopped and the routes stay applied, so traffic fails closed until the network returns.
    func enterWaiting() {
        policy.markWaiting()
        guard phase != .waiting else { return }
        phase = .waiting
        cycle += 1
        cancelTimers()
        host?.markReasserting(true)
        core.stop()
        announceTrouble(.transient)
        log.info("network unavailable, waiting")
    }

    // Every third failure in a row moves on to the next candidate, wrapping around.
    func retry(afterFailedAttempt attempt: Int, error: Error) {
        logFailure("attempt \(attempt)", error)
        note.set(TunnelStart.providerError(error).localizedDescription)
        host?.markReasserting(true)
        if latest?.usable == false { return enterWaiting() }
        let classified = ReconnectPolicy.classify(error)
        let missing = classified == .terminal(.noServer) && targets.ids.count > 1
        let failure: ReconnectPolicy.Failure = missing ? .transient : classified
        if case .terminal = failure { sessionReady = false }
        announceTrouble(failure)
        if missing || policy.rotates(afterFailedAttempt: attempt) { rotateTarget() }
        scheduleAttempt(attempt + 1, after: policy.delay(after: failure, attempt: attempt))
    }

    private func recheck() {
        recheckTimer = nil
        guard phase == .running || phase == .reconnecting || phase == .waiting, let latest else { return }
        handle(latest)
    }

    private func rotateTarget() {
        let next = targets.advanced()
        guard next != targets else { return }
        targets = next
        log.info("moving on to the next server")
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
        if attempt == 1, preparedExit == serverId, restartInPlace() {
            return attemptFinished(attempt, cycle: current, error: nil)
        }
        core.stop()
        establish(cycle: current, switching: false) { [weak self] error in
            self?.attemptFinished(attempt, cycle: current, error: error)
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
            log.info("reconnected on attempt \(attempt, privacy: .public)")
            return cameUp()
        }
        retry(afterFailedAttempt: attempt, error: error)
    }
}
