import Network
import NetworkExtension
import os
import Pangeacore

final class PacketTunnelProvider: NEPacketTunnelProvider, LifecycleHost {
    private let queue = DispatchQueue(label: "org.pangeavpn.tunnel.lifecycle")
    private let log = Logger(subsystem: "org.pangeavpn", category: "tunnel")
    private let note = ReconnectNote()
    private let activity = ProviderActivity()
    // Touched only on queue: a stop that lands mid-start must win.
    private var lifecycle: TunnelLifecycle?
    private var monitor: NWPathMonitor?

    override func startTunnel(
        options: [String: NSObject]?,
        completionHandler: @escaping (Error?) -> Void
    ) {
        activity.set(true)
        // NE hands over immutable NSString values and never touches them again, so the hop is safe.
        nonisolated(unsafe) let options = options
        queue.async {
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
        activity.set(false)
        let name = StopReasonName.describe(reason)
        queue.async {
            self.log.info("stopping, reason \(name, privacy: .public)")
            self.lifecycle?.stop()
            self.watchPath(false)
            completionHandler()
        }
    }

    // Completes at once: the lifecycle queue can be busy with a hub call for seconds.
    override func sleep(completionHandler: @escaping () -> Void) {
        log.info("device going to sleep")
        completionHandler()
    }

    override func wake() {
        queue.async { self.lifecycle?.wake() }
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        if let request = ProviderMessage.switchRequest(from: messageData) {
            return acceptSwitch(request, reply: completionHandler)
        }
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

    func availableMemory() -> Int {
        os_proc_available_memory()
    }

    private var excludesLocalNetworks: Bool? {
        (protocolConfiguration as? NETunnelProviderProtocol)?.excludeLocalNetworks
    }

    // Replies before the switch runs, since the lifecycle queue may be busy with a hub call.
    private func acceptSwitch(_ request: SwitchRequest, reply: ((Data?) -> Void)?) {
        let accepted = activity.isActive
        do {
            reply?(try JSONEncoder().encode(SwitchReply(accepted: accepted)))
        } catch {
            log.error("switch reply failed: \(error.localizedDescription, privacy: .private)")
            reply?(nil)
        }
        guard accepted else { return log.info("switch refused: the tunnel is not running") }
        queue.async { self.lifecycle?.switchServer(request) }
    }

    private func makeLifecycle() -> TunnelLifecycle {
        let alerts = ConnectionAlerts()
        return TunnelLifecycle(
            core: BridgedLifecycleCore(bridge: GoCore.shared(sink: nil)),
            host: self,
            scheduler: DispatchLifecycleScheduler(queue: queue),
            note: note,
            onEvent: { event in alerts.handle(event) })
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

// Switch requests are answered off the lifecycle queue, so whether one can run is kept here.
private final class ProviderActivity: @unchecked Sendable {
    private let lock = NSLock()
    private var active = false

    var isActive: Bool { lock.withLock { active } }

    func set(_ value: Bool) {
        lock.withLock { active = value }
    }
}

// Mapped by raw value, so a reason newer than the SDK still logs as a number.
private enum StopReasonName {
    private static let names: [Int: String] = [
        0: "none", 1: "userInitiated", 2: "providerFailed", 3: "noNetworkAvailable",
        4: "unrecoverableNetworkChange", 5: "providerDisabled", 6: "authenticationCanceled",
        7: "configurationFailed", 8: "idleTimeout", 9: "configurationDisabled", 10: "configurationRemoved",
        11: "superceded", 12: "userLogout", 13: "userSwitch", 14: "connectionFailed", 15: "sleep",
        16: "appUpdate", 17: "internalError",
    ]

    static func describe(_ reason: NEProviderStopReason) -> String {
        let raw = reason.rawValue
        return "\(names[raw] ?? "unknown") (\(raw))"
    }
}
