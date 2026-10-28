import NetworkExtension
import os
import Pangeacore

final class PacketTunnelProvider: NEPacketTunnelProvider {
    private let queue = DispatchQueue(label: "org.pangeavpn.tunnel.lifecycle")
    private let log = Logger(subsystem: "org.pangeavpn", category: "tunnel")
    // Touched only on queue: a stop that lands while settings apply must win.
    private var stopRequested = false

    private var core: CoreBridge { GoCore.shared(sink: nil) }

    override func startTunnel(
        options: [String: NSObject]?,
        completionHandler: @escaping (Error?) -> Void
    ) {
        queue.async {
            do {
                let settings = try self.restoreSettings()
                let serverId = try TunnelStart.resolveServerID(options: options, settings: settings)
                self.connect(serverId: serverId, allowLan: settings.allowLan, completion: completionHandler)
            } catch {
                self.fail(error, completionHandler)
            }
        }
    }

    override func stopTunnel(
        with reason: NEProviderStopReason,
        completionHandler: @escaping () -> Void
    ) {
        queue.async {
            self.stopRequested = true
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

    private func restoreSettings() throws -> AppSettings {
        do {
            _ = try core.restoreSession()
        } catch let error as CoreError {
            throw TunnelStart.mapRestoreError(error)
        }
        return try core.config()
    }

    private func connect(serverId: String, allowLan: Bool, completion: @escaping (Error?) -> Void) {
        do {
            let tunnel = try core.prepare(serverId: serverId)
            setTunnelNetworkSettings(TunnelSettingsBuilder.make(tunnel, allowLan: allowLan)) { error in
                self.queue.async { self.startCore(settingsError: error, completion: completion) }
            }
        } catch {
            fail(error, completion)
        }
    }

    // The utun only exists once the network settings have been applied.
    private func startCore(settingsError: Error?, completion: @escaping (Error?) -> Void) {
        if let settingsError { return fail(settingsError, completion) }
        guard !stopRequested else { return fail(CancellationError(), completion) }
        let fd = MobileFindTunFD()
        guard fd >= 0 else { return fail(TunnelStartError.noTunnelInterface, completion) }
        do {
            try core.start(tunFd: fd)
            completion(nil)
        } catch {
            fail(error, completion)
        }
    }

    private func fail(_ error: Error, _ completion: (Error?) -> Void) {
        let providerError = TunnelStart.providerError(error)
        log.error("start failed: \(providerError.localizedDescription, privacy: .public)")
        completion(providerError)
    }
}
