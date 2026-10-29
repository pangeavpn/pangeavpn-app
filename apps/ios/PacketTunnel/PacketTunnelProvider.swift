import Network
import NetworkExtension
import os
import Pangeacore

final class PacketTunnelProvider: NEPacketTunnelProvider, LifecycleHost {
    private let queue = DispatchQueue(label: "org.pangeavpn.tunnel.lifecycle")
    private let log = Logger(subsystem: "org.pangeavpn", category: "tunnel")
    private let note = ReconnectNote()
    // Touched only on queue: a stop that lands mid-start must win.
    private var lifecycle: TunnelLifecycle?
    private var monitor: NWPathMonitor?

    override func startTunnel(
        options: [String: NSObject]?,
        completionHandler: @escaping (Error?) -> Void
    ) {
        queue.async {
            // Probed before the core reads the store, which would take a locked Keychain for signed out.
            if let locked = KeychainProbe.lockedError() {
                self.log.error("start refused: the Keychain is locked")
                return completionHandler(TunnelStart.providerError(locked))
            }
            let lifecycle = self.lifecycle ?? self.makeLifecycle()
            self.lifecycle = lifecycle
            lifecycle.start(
                options: options,
                excludeLocalNetworks: self.excludesLocalNetworks,
                completion: completionHandler)
        }
    }

    override func stopTunnel(
        with reason: NEProviderStopReason,
        completionHandler: @escaping () -> Void
    ) {
        queue.async {
            self.log.info("stopping, reason \(reason.rawValue, privacy: .public)")
            self.lifecycle?.stop()
            self.watchPath(false)
            completionHandler()
        }
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        do {
            let core = GoCore.shared(sink: nil)
            completionHandler?(try ProviderMessage.reply(to: messageData, core: core, reconnectDetail: note.value))
        } catch {
            log.error("app message failed: \(error.localizedDescription, privacy: .private)")
            completionHandler?(nil)
        }
    }

    func applyTunnelSettings(_ settings: NEPacketTunnelNetworkSettings, completion: @escaping (Error?) -> Void) {
        setTunnelNetworkSettings(settings) { error in
            self.queue.async { completion(error) }
        }
    }

    func findTunFD() -> Int {
        MobileFindTunFD()
    }

    func markReasserting(_ value: Bool) {
        if reasserting != value { reasserting = value }
    }

    func watchPath(_ enabled: Bool) {
        guard enabled else {
            monitor?.cancel()
            monitor = nil
            return
        }
        guard monitor == nil else { return }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in self?.pathChanged(path) }
        self.monitor = monitor
        monitor.start(queue: queue)
    }

    private var excludesLocalNetworks: Bool? {
        (protocolConfiguration as? NETunnelProviderProtocol)?.excludeLocalNetworks
    }

    private func makeLifecycle() -> TunnelLifecycle {
        TunnelLifecycle(
            core: BridgedLifecycleCore(bridge: GoCore.shared(sink: nil)),
            host: self,
            scheduler: DispatchLifecycleScheduler(queue: queue),
            note: note)
    }

    private func pathChanged(_ path: Network.NWPath) {
        let interfaces = path.availableInterfaces.map { PathInterface(name: $0.name, type: $0.type) }
        let names = interfaces.map(\.name).joined(separator: ",")
        log.info("path \(String(describing: path.status), privacy: .public) via \(names, privacy: .public)")
        let update = PathUpdate(
            usable: path.status == .satisfied,
            key: NetworkKey.current(restrictedTo: interfaces))
        lifecycle?.pathChanged(update)
    }
}
