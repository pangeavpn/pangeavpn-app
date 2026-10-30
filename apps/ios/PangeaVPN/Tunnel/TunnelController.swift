import Foundation
import NetworkExtension
import os

@MainActor
final class TunnelController: TunnelControlling {
    private static let stopCheckInterval = Duration.milliseconds(100)
    private static let stopChecks = 50

    var status: TunnelStatus { monitor.status }
    var connectedDate: Date? { monitor.connectedDate }
    private(set) var onDemandActive = false
    var onChange: (() -> Void)?
    var onAutoConnectDeclined: (() -> Void)?

    private let log = Logger(subsystem: "org.pangeavpn", category: "controller")
    private let monitor: TunnelStatusMonitor
    private var manager: NETunnelProviderManager?
    private var configObserver: NSObjectProtocol?
    private var refreshTask: Task<Void, Never>?
    private var lastWrite: Task<Void, Error>?
    private var awaitingFirstConnect = false
    private var latestProfile: TunnelProfile?
    // When the tunnel session we last saw running with Connect On Demand on came up.
    private var armedSession: Date?

    // Loads at launch so a tunnel started earlier, or from Settings, shows its real state.
    init() {
        monitor = TunnelStatusMonitor()
        monitor.onChange = { [weak self] in self?.onChange?() }
        monitor.onTransition = { [weak self] previous, current in
            self?.transitioned(from: previous, to: current)
        }
        configObserver = NotificationCenter.default.addObserver(
            forName: .NEVPNConfigurationChange, object: nil, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { () -> Void in self?.scheduleRefresh() }
        }
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

    // A stop with nothing to stop, or one that hung, can leave the drop rule up with no tunnel.
    nonisolated static func mustDisable(includeAllNetworks: Bool, wasRunning: Bool, stopped: Bool) -> Bool {
        includeAllNetworks && (!wasRunning || !stopped)
    }

    func connect(serverId: String, candidates: [String], profile: TunnelProfile) async throws {
        latestProfile = profile
        let write = enqueue { [weak self] in
            try await self?.start(serverId: serverId, candidates: candidates, profile: profile)
        }
        try await write.value
    }

    func switchServer(serverId: String, candidates: [String]) async throws {
        guard let session = await runningSession() else { throw TunnelSwitchError.notRunning }
        let message = try ProviderMessage.encodeSwitch(SwitchRequest(serverId: serverId, candidates: candidates))
        let reply = try await ProviderChannel.send(message, on: session)
        guard ProviderChannel.accepted(reply) else {
            log.error("the tunnel refused the server switch")
            throw TunnelSwitchError.rejected
        }
    }

    // Also the foreground reconcile. includeAllNetworks applies live; enforceRoutes and LAN from the next connect.
    func updateProfile(_ profile: TunnelProfile) async {
        latestProfile = profile
        let write = enqueue { [weak self] in await self?.reconcile(profile) }
        _ = await write.result
    }

    func disconnect() async {
        awaitingFirstConnect = false
        let write = enqueue { [weak self] in await self?.stop() }
        _ = await write.result
    }

    // Sign-out leaves no profile that Settings could start, only to fail signed out behind the kill switch.
    func removeConfiguration() async {
        awaitingFirstConnect = false
        let write = enqueue { [weak self] in await self?.remove() }
        _ = await write.result
    }

    func extensionLogs() async -> String? {
        guard let session = await runningSession() else { return nil }
        do {
            guard let reply = try await ProviderChannel.send(ProviderChannel.logsRequest, on: session) else {
                log.error("the tunnel sent no logs")
                return nil
            }
            return ProviderChannel.text(reply)
        } catch {
            log.error("could not read the tunnel logs (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            return nil
        }
    }

    // Configuration writes run one at a time, so a late save can never undo a newer one.
    private func enqueue(_ body: @escaping @MainActor () async throws -> Void) -> Task<Void, Error> {
        let previous = lastWrite
        let task = Task { @MainActor in
            _ = await previous?.result
            try await body()
        }
        lastWrite = task
        return task
    }

    // Saved with on-demand off so a failing first connect shows its error rather than retrying forever.
    private func start(serverId: String, candidates: [String], profile: TunnelProfile) async throws {
        let manager = try await reloadManager()
        armedSession = nil
        TunnelConfigurator.apply(profile, onDemand: false, to: manager)
        // Also undoes the disable that a kill-switch release may have saved.
        manager.isEnabled = true
        do {
            try await manager.saveToPreferences()
            try await manager.loadFromPreferences()
            attach(manager)
            monitor.stopRequested = false
            awaitingFirstConnect = true
            try manager.connection.startVPNTunnel(
                options: TunnelConfigurator.startOptions(serverId: serverId, candidates: candidates))
        } catch {
            awaitingFirstConnect = false
            forgetManager()
            throw error
        }
        if Self.state(for: manager.connection.status) == .connected { firstConnected() }
    }

    private func reconcile(_ requested: TunnelProfile) async {
        let manager: NETunnelProviderManager
        do {
            // Nothing saved yet: the first connect creates it, so a foreground never prompts.
            guard let saved = try await loadSaved() else { return }
            manager = saved
        } catch {
            log.error("could not load the VPN configuration to update it (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            return
        }
        let state = Self.state(for: manager.connection.status)
        var profile = requested
        if OnDemandPolicy.declinedOutside(
            requested, enabled: manager.isOnDemandEnabled, configurationEnabled: manager.isEnabled,
            armedSessionUp: state == .connected && isArmedSession(manager.connection)) {
            log.info("Connect On Demand was turned off in iOS Settings; auto-connect follows")
            profile.autoConnect = false
            onAutoConnectDeclined?()
        }
        let onDemand = OnDemandPolicy.reconciled(
            profile, state: state, enabled: manager.isOnDemandEnabled, awaitingFirstConnect: awaitingFirstConnect)
        guard !TunnelConfigurator.matches(manager, profile: profile, onDemand: onDemand) else {
            rememberSession(manager)
            return
        }
        TunnelConfigurator.apply(profile, onDemand: onDemand, to: manager)
        do {
            try await manager.saveToPreferences()
            noteSaved(manager)
        } catch {
            log.error("could not save the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            scheduleRefresh()
        }
    }

    // Stops even an idle tunnel: an app-side stop is how Apple says to clear a stranded drop rule.
    private func stop() async {
        guard let manager = await managerToStop() else { return }
        let connection = manager.connection
        let wasRunning = Self.state(for: connection.status) != .disconnected
        // Set first: turning on-demand off can fall back to a disable, which stops the tunnel itself.
        monitor.stopRequested = true
        if manager.isOnDemandEnabled { await turnOffOnDemand(manager) }
        connection.stopVPNTunnel()
        let stopped: Bool
        if wasRunning {
            stopped = await waitUntilStopped(connection)
        } else {
            stopped = true
        }
        let blocksAll = manager.protocolConfiguration?.includeAllNetworks == true
        if Self.mustDisable(includeAllNetworks: blocksAll, wasRunning: wasRunning, stopped: stopped) {
            await disable(manager, onlyIfBlockingAll: true)
        }
        if !wasRunning { monitor.settleIdleRelease() }
    }

    private func managerToStop() async -> NETunnelProviderManager? {
        do {
            return try await reloadManager()
        } catch {
            log.error("disconnect could not reload the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            return manager
        }
    }

    // Saved before the stop, or iOS would start the tunnel straight back up.
    private func turnOffOnDemand(_ manager: NETunnelProviderManager) async {
        manager.isOnDemandEnabled = false
        do {
            try await manager.saveToPreferences()
            noteSaved(manager)
        } catch {
            log.error("could not turn off Connect On Demand (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            await disable(manager, onlyIfBlockingAll: false)
        }
    }

    // A disabled configuration has no drop rule and no on-demand; the next connect re-enables it.
    private func disable(_ manager: NETunnelProviderManager, onlyIfBlockingAll: Bool) async {
        do {
            try await manager.loadFromPreferences()
            if onlyIfBlockingAll, manager.protocolConfiguration?.includeAllNetworks != true { return }
            manager.isOnDemandEnabled = false
            manager.isEnabled = false
            try await manager.saveToPreferences()
            noteSaved(manager)
        } catch {
            log.error("could not disable the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            scheduleRefresh()
        }
    }

    private func remove() async {
        monitor.stopRequested = true
        do {
            let saved = try await NETunnelProviderManager.loadAllFromPreferences()
            for found in saved where TunnelConfigurator.isOurs(found) {
                if found.isOnDemandEnabled { await turnOffOnDemand(found) }
                try await found.removeFromPreferences()
            }
        } catch {
            log.error("could not remove the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
        }
        forgetManager()
        monitor.reset()
        onDemandActive = false
        armedSession = nil
        onChange?()
    }

    private func transitioned(from previous: TunnelState, to current: TunnelState) {
        if current == .connected {
            firstConnected()
        } else if previous == .connecting, current == .disconnected {
            awaitingFirstConnect = false
        }
    }

    // Turned on only after the app's own connect succeeds, so a failed start never loops on demand.
    private func firstConnected() {
        guard awaitingFirstConnect else { return }
        awaitingFirstConnect = false
        guard let profile = latestProfile, OnDemandPolicy.wanted(profile) else { return }
        _ = enqueue { [weak self] in await self?.reconcile(profile) }
    }

    private func runningSession() async -> NETunnelProviderSession? {
        let found: NETunnelProviderManager
        do {
            found = try await loadManager()
        } catch {
            log.error("could not load the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            return nil
        }
        guard let session = found.connection as? NETunnelProviderSession,
              ProviderChannel.canMessage(session.status) else { return nil }
        return session
    }

    private func noteSaved(_ manager: NETunnelProviderManager) {
        rememberSession(manager)
        onDemandActive = OnDemandPolicy.active(
            configurationEnabled: manager.isEnabled, onDemandEnabled: manager.isOnDemandEnabled)
        onChange?()
    }

    // Only our own writes and checks set this, never a refresh, so an outside switch-off stays visible.
    private func rememberSession(_ manager: NETunnelProviderManager) {
        let connection = manager.connection
        let armed = manager.isOnDemandEnabled && Self.state(for: connection.status) == .connected
        armedSession = armed ? connection.connectedDate : nil
    }

    // Loose, because each loaded copy of the configuration reads the date from the system afresh.
    private func isArmedSession(_ connection: NEVPNConnection) -> Bool {
        guard let armedSession, let current = connection.connectedDate else { return false }
        return abs(current.timeIntervalSince(armedSession)) < 1
    }

    // iOS Settings, or another VPN app taking over, can change the saved configuration under us.
    private func scheduleRefresh() {
        refreshTask?.cancel()
        refreshTask = Task { [weak self] in await self?.refreshFromPreferences() }
    }

    private func refreshFromPreferences() async {
        do {
            let found = try await Self.findOrCreateManager()
            guard !Task.isCancelled else { return }
            attach(found)
        } catch {
            log.error("could not reload the VPN configuration (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
        }
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

    private func loadSaved() async throws -> NETunnelProviderManager? {
        let saved = try await NETunnelProviderManager.loadAllFromPreferences()
        guard let ours = saved.first(where: { TunnelConfigurator.isOurs($0) }) else { return nil }
        attach(ours)
        return ours
    }

    private static func findOrCreateManager() async throws -> NETunnelProviderManager {
        let saved = try await NETunnelProviderManager.loadAllFromPreferences()
        return saved.first { TunnelConfigurator.isOurs($0) } ?? NETunnelProviderManager()
    }

    // Reloads in the background so status keeps tracking the saved configuration.
    private func forgetManager() {
        monitor.forget()
        manager = nil
        Task { [weak self] in await self?.loadExisting() }
    }

    // The monitor always watches this manager's connection, never a stale copy's.
    private func attach(_ manager: NETunnelProviderManager) {
        self.manager = manager
        onDemandActive = OnDemandPolicy.active(
            configurationEnabled: manager.isEnabled, onDemandEnabled: manager.isOnDemandEnabled)
        monitor.watch(manager.connection)
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
