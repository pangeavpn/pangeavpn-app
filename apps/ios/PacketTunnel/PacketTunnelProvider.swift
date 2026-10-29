import Network
import NetworkExtension
import os
import Pangeacore

private struct Superseded: Error {}

final class PacketTunnelProvider: NEPacketTunnelProvider {
    private let queue = DispatchQueue(label: "org.pangeavpn.tunnel.lifecycle")
    private let log = Logger(subsystem: "org.pangeavpn", category: "tunnel")
    // Everything below is touched only on queue: a stop that lands mid-start must win.
    private var stopRequested = false
    private var serverId = ""
    private var allowLan = false
    private var policy = ReconnectPolicy()
    private var monitor: NWPathMonitor?
    private var pending: DispatchWorkItem?
    private var cycle = 0

    private var core: CoreBridge { GoCore.shared(sink: nil) }

    override func startTunnel(
        options: [String: NSObject]?,
        completionHandler: @escaping (Error?) -> Void
    ) {
        queue.async {
            do {
                let settings = try self.restoreSessionAndConfig()
                self.serverId = try TunnelStart.resolveServerID(options: options, settings: settings)
                self.allowLan = settings.allowLan
                self.establish(cycle: self.cycle) { self.finishStart($0, completionHandler) }
            } catch {
                self.finishStart(error, completionHandler)
            }
        }
    }

    override func stopTunnel(
        with reason: NEProviderStopReason,
        completionHandler: @escaping () -> Void
    ) {
        queue.async {
            self.stopRequested = true
            self.monitor?.cancel()
            self.monitor = nil
            self.pending?.cancel()
            self.pending = nil
            self.log.info("stopping, reason \(reason.rawValue, privacy: .public)")
            self.core.stop()
            completionHandler()
        }
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        do {
            completionHandler?(try ProviderMessage.reply(to: messageData, core: core))
        } catch {
            log.error("app message failed: \(error.localizedDescription, privacy: .public)")
            completionHandler?(nil)
        }
    }

    private func restoreSessionAndConfig() throws -> AppSettings {
        do {
            _ = try core.restoreSession()
        } catch let error as CoreError {
            throw TunnelStart.mapRestoreError(error)
        }
        return try core.config()
    }

    // Shared by the first start and every reconnect; completion runs on queue.
    private func establish(cycle: Int, _ completion: @escaping (Error?) -> Void) {
        do {
            let tunnel = try core.prepare(serverId: serverId)
            setTunnelNetworkSettings(TunnelSettingsBuilder.make(tunnel, allowLan: allowLan)) { error in
                self.queue.async { self.startCore(settingsError: error, cycle: cycle, completion: completion) }
            }
        } catch {
            completion(error)
        }
    }

    // The utun only exists once the network settings have been applied.
    private func startCore(settingsError: Error?, cycle: Int, completion: @escaping (Error?) -> Void) {
        if let settingsError { return completion(settingsError) }
        guard !stopRequested else { return completion(TunnelStartError.stopped) }
        guard cycle == self.cycle else { return completion(Superseded()) }
        let fd = MobileFindTunFD()
        guard fd >= 0 else { return completion(TunnelStartError.noTunnelInterface) }
        do {
            try core.start(tunFd: fd)
            completion(nil)
        } catch {
            completion(error)
        }
    }

    private func finishStart(_ error: Error?, _ completion: @escaping (Error?) -> Void) {
        guard let error else {
            startMonitor()
            return completion(nil)
        }
        let mapped = TunnelStart.providerError(error)
        if let stopped = error as? TunnelStartError, stopped == .stopped {
            log.info("start abandoned: stop requested")
        } else {
            log.error("start failed: \(mapped.localizedDescription, privacy: .public)")
        }
        completion(mapped)
    }

    private func startMonitor() {
        guard !stopRequested else { return }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            self?.handle(usable: path.status == .satisfied)
        }
        self.monitor = monitor
        monitor.start(queue: queue)
    }

    private func handle(usable: Bool) {
        guard !stopRequested else { return }
        let decision = policy.pathChanged(key: NetworkKey.current(), usable: usable)
        if decision == .ignore { return }
        pending?.cancel()
        pending = nil
        cycle += 1
        guard case .reconnect(let delay) = decision else {
            return log.info("network unavailable, waiting")
        }
        log.info("network changed, reconnecting in \(delay, privacy: .public)s")
        schedule(after: delay, attempt: 1, cycle: cycle)
    }

    private func schedule(after delay: TimeInterval, attempt: Int, cycle: Int) {
        let item = DispatchWorkItem { [weak self] in
            self?.reconnect(attempt: attempt, cycle: cycle)
        }
        pending = item
        queue.asyncAfter(deadline: .now() + delay, execute: item)
    }

    private func reconnect(attempt: Int, cycle: Int) {
        guard !stopRequested, cycle == self.cycle else { return }
        reasserting = true
        core.stop()
        establish(cycle: cycle) { error in
            guard !self.stopRequested, cycle == self.cycle else { return }
            guard let error else {
                self.reasserting = false
                return self.log.info("reconnected on attempt \(attempt, privacy: .public)")
            }
            self.log.error("reconnect attempt \(attempt, privacy: .public) failed: \(TunnelStart.providerError(error).localizedDescription, privacy: .public)")
            self.retryOrGiveUp(after: attempt, cycle: cycle)
        }
    }

    private func retryOrGiveUp(after attempt: Int, cycle: Int) {
        guard attempt < policy.maxAttempts else {
            stopRequested = true
            monitor?.cancel()
            monitor = nil
            pending?.cancel()
            pending = nil
            return cancelTunnelWithError(TunnelStart.providerError(TunnelStartError.reconnectFailed))
        }
        schedule(after: policy.backoff(attempt: attempt), attempt: attempt + 1, cycle: cycle)
    }
}
