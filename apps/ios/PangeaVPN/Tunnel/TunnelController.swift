import Foundation
import NetworkExtension
import os

@MainActor
final class TunnelController: TunnelControlling {
    static let providerBundleIdentifier = "org.pangeavpn.app.tunnel"
    private static let displayName = "PangeaVPN"
    private static let pollInterval = Duration.seconds(1)
    private static let stopCheckInterval = Duration.milliseconds(100)
    private static let stopChecks = 50

    private(set) var status = TunnelStatus.disconnected
    private(set) var connectedDate: Date?
    var onChange: (() -> Void)?

    private let log = Logger(subsystem: "org.pangeavpn", category: "controller")
    private var manager: NETunnelProviderManager?
    private var observer: NSObjectProtocol?
    private var pollTask: Task<Void, Never>?
    private var neState = TunnelState.disconnected
    private var stopRequested = false

    // Loads at launch so a tunnel started earlier, or from Settings, shows its real state.
    init() {
        Task { [weak self] in await self?.loadExisting() }
    }

    nonisolated static func state(for status: NEVPNStatus) -> TunnelState {
        switch status {
        case .invalid, .disconnected: return .disconnected
        case .connecting, .reasserting: return .connecting
        case .connected: return .connected
        case .disconnecting: return .disconnecting
        @unknown default: return .disconnected
        }
    }

    func connect(serverId: String, killSwitch: Bool, allowLan: Bool) async throws {
        let manager = try await reloadManager()
        Self.configure(manager, killSwitch: killSwitch, allowLan: allowLan)
        do {
            try await manager.saveToPreferences()
            try await manager.loadFromPreferences()
            observe(manager.connection)
            stopRequested = false
            try manager.connection.startVPNTunnel(options: [TunnelStart.serverIdOption: serverId as NSString])
        } catch {
            forgetManager()
            throw error
        }
    }

    // Stops even an idle tunnel: an app-side stop is how Apple says to clear a stranded drop rule.
    func disconnect() async {
        let manager: NETunnelProviderManager
        do {
            manager = try await loadManager()
        } catch {
            log.error("disconnect could not load the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            return
        }
        let connection = manager.connection
        let wasRunning = Self.state(for: connection.status) != .disconnected
        stopRequested = true
        connection.stopVPNTunnel()
        let stopped: Bool
        if wasRunning {
            stopped = await waitUntilStopped(connection)
        } else {
            stopped = true
        }
        let blocksAll = manager.protocolConfiguration?.includeAllNetworks == true
        if Self.mustDisable(includeAllNetworks: blocksAll, wasRunning: wasRunning, stopped: stopped) {
            await disable(manager)
        }
        if !wasRunning { settleIdleRelease() }
    }

    // A stop with nothing to stop, or one that hung, can leave the drop rule up with no tunnel.
    nonisolated static func mustDisable(includeAllNetworks: Bool, wasRunning: Bool, stopped: Bool) -> Bool {
        includeAllNetworks && (!wasRunning || !stopped)
    }

    // Sign-out leaves no profile that Settings could start, only to fail signed out behind the kill switch.
    func removeConfiguration() async {
        stopRequested = true
        do {
            let saved = try await NETunnelProviderManager.loadAllFromPreferences()
            for found in saved where Self.isOurs(found) {
                try await found.removeFromPreferences()
            }
        } catch {
            log.error("could not remove the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
        }
        forgetManager()
        neState = .disconnected
        stopRequested = false
        connectedDate = nil
        status = .disconnected
        onChange?()
    }

    // Apple ties the kill switch's drop rule to an enabled configuration; the next connect re-enables it.
    private func disable(_ manager: NETunnelProviderManager) async {
        do {
            try await manager.loadFromPreferences()
            guard manager.protocolConfiguration?.includeAllNetworks == true else { return }
            manager.isEnabled = false
            try await manager.saveToPreferences()
        } catch {
            log.error("could not disable the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
        }
    }

    // Stopping an idle tunnel fires no NE transition, so nothing else would consume the flag.
    private func settleIdleRelease() {
        stopRequested = false
        guard status.state == .error else { return }
        status = .disconnected
        onChange?()
    }

    private func loadExisting() async {
        do {
            _ = try await loadManager()
        } catch {
            log.error("could not load the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
        }
    }

    private func loadManager() async throws -> NETunnelProviderManager {
        if let manager { return manager }
        let found = try await Self.findOrCreateManager()
        // Another caller may have attached a manager while this load was in flight.
        if let manager { return manager }
        attach(found)
        return found
    }

    // Another VPN app or Settings may have changed the saved copy, and saving a stale one fails.
    private func reloadManager() async throws -> NETunnelProviderManager {
        let found = try await Self.findOrCreateManager()
        attach(found)
        return found
    }

    private static func findOrCreateManager() async throws -> NETunnelProviderManager {
        let saved = try await NETunnelProviderManager.loadAllFromPreferences()
        return saved.first { isOurs($0) } ?? NETunnelProviderManager()
    }

    // Reloads in the background so status keeps tracking the saved configuration.
    private func forgetManager() {
        if let observer { NotificationCenter.default.removeObserver(observer) }
        observer = nil
        stopPolling()
        manager = nil
        Task { [weak self] in await self?.loadExisting() }
    }

    private static func isOurs(_ manager: NETunnelProviderManager) -> Bool {
        let proto = manager.protocolConfiguration as? NETunnelProviderProtocol
        return proto?.providerBundleIdentifier == providerBundleIdentifier
    }

    private static func configure(_ manager: NETunnelProviderManager, killSwitch: Bool, allowLan: Bool) {
        let proto = NETunnelProviderProtocol()
        proto.providerBundleIdentifier = providerBundleIdentifier
        proto.serverAddress = displayName
        proto.includeAllNetworks = killSwitch
        proto.excludeLocalNetworks = allowLan
        manager.protocolConfiguration = proto
        manager.localizedDescription = displayName
        // Also undoes the disable that a kill-switch release may have saved.
        manager.isEnabled = true
    }

    private func attach(_ manager: NETunnelProviderManager) {
        self.manager = manager
        observe(manager.connection)
        statusChanged()
    }

    private func observe(_ connection: NEVPNConnection) {
        if let observer { NotificationCenter.default.removeObserver(observer) }
        observer = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange, object: connection, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { () -> Void in self?.statusChanged() }
        }
    }

    private func statusChanged() {
        guard let connection = manager?.connection else { return }
        let previous = neState
        neState = Self.state(for: connection.status)
        connectedDate = neState == .connected ? connection.connectedDate : nil
        switch neState {
        case .connecting, .connected:
            var next = previous == .disconnected ? TunnelStatus.disconnected : status
            next.state = neState
            status = next
            startPolling()
        case .disconnecting:
            stopPolling()
            status.state = .disconnecting
        case .disconnected, .error:
            stopPolling()
            if previous != .disconnected {
                status = .disconnected
                reportDisconnect()
            }
        }
        onChange?()
    }

    private func reportDisconnect() {
        let requested = stopRequested
        stopRequested = false
        Task { [weak self] in
            guard let connection = self?.manager?.connection else { return }
            let error = await TunnelController.lastDisconnectError(of: connection)
            self?.showDisconnectError(error, requested: requested)
        }
    }

    private func showDisconnectError(_ error: Error?, requested: Bool) {
        guard let error, !requested, neState == .disconnected, !TunnelStart.isStopped(error) else { return }
        var next = TunnelStatus.disconnected
        next.state = .error
        next.detail = error.localizedDescription
        status = next
        onChange?()
    }

    private static func lastDisconnectError(of connection: NEVPNConnection) async -> Error? {
        await withCheckedContinuation { continuation in
            connection.fetchLastDisconnectError { continuation.resume(returning: $0) }
        }
    }

    private func startPolling() {
        guard pollTask == nil else { return }
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                await self.poll()
                do {
                    try await Task.sleep(for: TunnelController.pollInterval)
                } catch {
                    return
                }
            }
        }
    }

    private func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    private func poll() async {
        guard let session = manager?.connection as? NETunnelProviderSession else { return }
        do {
            let reply = try await Self.send(Data([ProviderMessage.state.rawValue]), on: session)
            let polled = try JSONDecoder().decode(TunnelStatus.self, from: reply)
            apply(polled)
        } catch {
            log.debug("status poll failed (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
        }
    }

    // NE owns the state; the core's status only fills in detail, bytes, transport and server.
    private func apply(_ polled: TunnelStatus) {
        guard neState == .connecting || neState == .connected else { return }
        var next = polled
        next.state = neState
        guard next != status else { return }
        status = next
        onChange?()
    }

    private static func send(_ message: Data, on session: NETunnelProviderSession) async throws -> Data {
        try await withCheckedThrowingContinuation { continuation in
            do {
                try session.sendProviderMessage(message) { reply in
                    guard let reply else { return continuation.resume(throwing: PollError.noReply) }
                    continuation.resume(returning: reply)
                }
            } catch {
                continuation.resume(throwing: error)
            }
        }
    }

    private func waitUntilStopped(_ connection: NEVPNConnection) async -> Bool {
        for _ in 0..<Self.stopChecks {
            if Self.state(for: connection.status) == .disconnected { return true }
            do {
                try await Task.sleep(for: Self.stopCheckInterval)
            } catch {
                break
            }
        }
        let stopped = Self.state(for: connection.status) == .disconnected
        if !stopped { log.info("tunnel still stopping after the wait") }
        return stopped
    }
}

private enum PollError: Error {
    case noReply
}
