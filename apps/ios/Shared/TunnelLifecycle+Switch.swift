import Foundation
import os

// A switch never leaves the VPN session: it stays connected or reasserting throughout.
extension TunnelLifecycle {
    func switchServer(_ request: SwitchRequest) {
        guard !request.serverId.isEmpty else { return }
        switch phase {
        case .idle, .stopped:
            log.info("switch ignored: the tunnel is not running")
        case .starting:
            supersedeStart(with: request)
        case .running, .reconnecting, .waiting:
            beginSwitch(to: request)
        }
    }

    private func retarget(_ request: SwitchRequest) {
        let optionServer = startOptions?[TunnelStart.serverIdOption] as? String ?? ""
        let previous = serverId.isEmpty ? optionServer : serverId
        targets = CandidateList.forSwitch(to: request.serverId, from: previous, listed: request.candidates)
    }

    // Keeps the app's completion, so the start it waits on finishes on the new server instead.
    private func supersedeStart(with request: SwitchRequest) {
        guard let pending = pendingStart else { return }
        retarget(request)
        cycle += 1
        pendingStart = PendingStart(cycle: cycle, completion: pending.completion)
        log.info("switch replaces the pending start")
        guard blockingDone else { return }
        runStartSequence(cycle: cycle)
    }

    // Reasserting goes up before the prepare, because the hub drops the old peer once it succeeds.
    private func beginSwitch(to request: SwitchRequest) {
        retarget(request)
        guard phase != .waiting else {
            return log.info("switch saved for when the network returns")
        }
        phase = .reconnecting
        cycle += 1
        cancelTimers()
        host?.markReasserting(true)
        log.info("switching server in place")
        let current = cycle
        establish(cycle: current, switching: true) { [weak self] error in
            self?.switchFinished(error, cycle: current)
        }
    }

    private func switchFinished(_ error: Error?, cycle current: Int) {
        guard phase == .reconnecting, current == cycle else { return }
        guard let error else {
            log.info("switched server in place")
            return cameUp()
        }
        retry(afterFailedAttempt: 1, error: error)
    }
}
