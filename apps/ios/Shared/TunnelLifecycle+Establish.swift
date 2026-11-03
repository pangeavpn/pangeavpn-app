import Foundation
import NetworkExtension
import os

enum MultihopChoice: Equatable {
    case direct
    case via(String)

    // nil is single hop; "" or the exit itself means the lightest entry the core offers.
    static func choose(preference: String?, exit: String, entries: () -> [String]) -> MultihopChoice {
        guard let preference else { return .direct }
        if !preference.isEmpty, preference != exit { return .via(preference) }
        guard let entry = entries().first else { return .direct }
        return .via(entry)
    }
}

extension TunnelLifecycle {
    func applyBlocking(session started: Int) {
        let ranges = SharedPrefs.excludedRanges(in: prefs)
        let blocking = TunnelSettingsBuilder.blockingConfig
        let fingerprint = TunnelSettingsBuilder.fingerprint(blocking, allowLan: allowLan, excludedRanges: ranges)
        let settings = TunnelSettingsBuilder.blocking(allowLan: allowLan, excludedRanges: ranges)
        applySettings(settings, fingerprint: fingerprint) { [weak self] error in
            self?.blockingApplied(error, session: started)
        }
    }

    // Restores once per session; an account problem clears it so the next attempt restores again.
    func ensureSession() throws {
        guard !sessionReady else { return }
        if let locked = core.keychainLocked() { throw locked }
        let servers = try restoreSession()
        let settings = try core.config()
        allowLan = TunnelStart.resolveAllowLan(excludeLocalNetworks: excludeLocalNetworks, settings: settings)
        if targets.isEmpty {
            let chosen = try TunnelStart.resolveServerID(options: startOptions, settings: settings)
            targets = CandidateList(TunnelStart.startCandidates(
                options: startOptions, serverId: chosen, transport: settings.preferredTransport, servers: servers))
        }
        sessionReady = true
    }

    // A switch stops Go only after the prepare, since the hub keeps the old peer until then.
    func establish(cycle current: Int, switching: Bool, _ completion: @escaping (Error?) -> Void) {
        do {
            try ensureSession()
            let tunnel = try prepareRoute()
            let ranges = SharedPrefs.excludedRanges(in: prefs)
            let settings = try TunnelSettingsBuilder.build(tunnel, allowLan: allowLan, excludedRanges: ranges)
            let fingerprint = TunnelSettingsBuilder.fingerprint(tunnel, allowLan: allowLan, excludedRanges: ranges)
            if switching { core.stopForSwitch() }
            guard fingerprint != appliedFingerprint else {
                return startCore(settingsError: nil, cycle: current, completion: completion)
            }
            applySettings(settings, fingerprint: fingerprint) { [weak self] error in
                self?.startCore(settingsError: error, cycle: current, completion: completion)
            }
        } catch {
            completion(error)
        }
    }

    // Every prepare reads the multihop choice, so a reconnect keeps the route the user picked.
    func prepareRoute() throws -> TunnelConfig {
        let exit = serverId
        let preference = SharedPrefs.multihopEntry(in: prefs)
        let core = self.core
        let choice = MultihopChoice.choose(preference: preference, exit: exit) {
            core.multihopEntries(exitId: exit)
        }
        let tunnel: TunnelConfig
        switch choice {
        case .direct:
            if preference != nil { log.info("no multihop entry available, connecting directly") }
            tunnel = try core.prepare(serverId: exit)
        case .via(let entry):
            tunnel = try core.prepareVia(exitId: exit, entryId: entry)
        }
        preparedExit = exit
        return tunnel
    }

    private func restoreSession() throws -> [Server] {
        do {
            return try core.restore()
        } catch let error as CoreError {
            throw TunnelStart.mapRestoreError(error)
        }
    }

    // A failed or unanswered apply leaves the state unknown, so the next one is never skipped.
    private func applySettings(
        _ settings: NEPacketTunnelNetworkSettings,
        fingerprint: TunnelSettingsFingerprint,
        completion: @escaping (Error?) -> Void
    ) {
        guard let host else { return completion(TunnelStartError.stopped) }
        appliedFingerprint = nil
        policy.settingsApplied(at: scheduler.now)
        SettingsApply.run(
            timeout: settingsTimeout,
            scheduler: scheduler,
            apply: { done in host.applyTunnelSettings(settings, completion: done) },
            completion: { [weak self] error in
                guard let self else { return completion(error) }
                self.policy.settingsApplied(at: self.scheduler.now)
                if error == nil { self.appliedFingerprint = fingerprint }
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
}
