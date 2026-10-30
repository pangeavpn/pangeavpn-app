import Foundation
import Observation
import os

@MainActor
@Observable
final class SettingsStore {
    static let killSwitchKey = SharedPrefs.Key.killSwitch
    static let darkThemeKey = SharedPrefs.Key.darkTheme

    private(set) var settings = AppSettings()
    private(set) var loaded = false
    // The strict kill switch (includeAllNetworks); the VPN profile carries it, not the core.
    private(set) var killSwitch = false
    // nil follows the system appearance.
    private(set) var darkTheme: Bool?
    private(set) var autoConnect = true
    private(set) var lockdown = false
    private(set) var connectionAlerts = true
    private(set) var panicWipe = false
    private(set) var accent = Accent.terra
    private(set) var developerMode = false
    private(set) var splitTunnelEnabled = false
    private(set) var splitTunnelRanges: [String] = []
    private(set) var multihopEnabled = false
    // nil picks the lightest entry outside the exit's region.
    private(set) var multihopEntryId: String?
    var error: String?

    private let core: any CoreBridge
    private let defaults: UserDefaults

    private static let log = Logger(subsystem: "org.pangeavpn", category: "settings")

    init(core: any CoreBridge, defaults: UserDefaults = AppGroup.defaults) {
        self.core = core
        self.defaults = defaults
        typealias Key = SharedPrefs.Key
        killSwitch = defaults.bool(forKey: Key.killSwitch)
        darkTheme = defaults.object(forKey: Key.darkTheme) as? Bool
        autoConnect = SharedPrefs.bool(Key.autoConnect, default: true, in: defaults)
        lockdown = SharedPrefs.bool(Key.lockdown, default: false, in: defaults)
        connectionAlerts = SharedPrefs.connectionAlerts(in: defaults)
        panicWipe = SharedPrefs.bool(Key.panicWipe, default: false, in: defaults)
        accent = defaults.string(forKey: Key.accent).flatMap(Accent.init(rawValue:)) ?? .terra
        developerMode = SharedPrefs.bool(Key.developerMode, default: false, in: defaults)
        splitTunnelEnabled = SharedPrefs.bool(Key.splitTunnelEnabled, default: false, in: defaults)
        splitTunnelRanges = SharedPrefs.strings(Key.splitTunnelRanges, in: defaults)
        multihopEnabled = SharedPrefs.bool(Key.multihopEnabled, default: false, in: defaults)
        multihopEntryId = defaults.string(forKey: Key.multihopEntryId).flatMap { $0.isEmpty ? nil : $0 }
        AccentTheme.shared.accent = accent
    }

    var tunnelProfile: TunnelProfile {
        TunnelProfile(strictKillSwitch: killSwitch, allowLan: settings.allowLan,
                      autoConnect: autoConnect, lockdown: lockdown)
    }

    // Queued with the edits, so a slow load cannot land after an edit and undo it.
    func load() async {
        let core = self.core
        do {
            settings = try await CoreQueue.run(on: CoreQueue.settings) { try core.config() }
            loaded = true
            error = nil
        } catch {
            show(error, doing: "load")
        }
    }

    func setPreferredTransport(_ transport: Transport) async {
        let raw = transport.rawValue
        await edit { $0.preferredTransport = raw }
    }

    func setCustomDns(_ text: String) async {
        let servers = Self.dnsServers(in: text)
        await edit { $0.customDns = servers }
    }

    func setMtu(_ mtu: Int) async {
        await edit { $0.mtu = mtu }
    }

    func setAllowLan(_ enabled: Bool) async {
        await edit { $0.allowLan = enabled }
    }

    func setDeadDrop(_ enabled: Bool) async {
        await edit { $0.deadDrop = enabled }
    }

    // The core enforces the last-method rule, so a refusal arrives as an error.
    func setHubMethod(_ method: String, enabled: Bool) async {
        let core = self.core
        await store { try core.setHubMethod(method, enabled: enabled) }
    }

    func setKillSwitch(_ enabled: Bool) {
        defaults.set(enabled, forKey: Self.killSwitchKey)
        killSwitch = enabled
    }

    func setDarkTheme(_ dark: Bool?) {
        if let dark {
            defaults.set(dark, forKey: Self.darkThemeKey)
        } else {
            defaults.removeObject(forKey: Self.darkThemeKey)
        }
        darkTheme = dark
    }

    func setPostQuantum(_ enabled: Bool) async {
        await edit { $0.postQuantum = enabled }
    }

    func setAutoConnect(_ enabled: Bool) {
        defaults.set(enabled, forKey: SharedPrefs.Key.autoConnect)
        autoConnect = enabled
    }

    func setLockdown(_ enabled: Bool) {
        defaults.set(enabled, forKey: SharedPrefs.Key.lockdown)
        lockdown = enabled
    }

    func setConnectionAlerts(_ enabled: Bool) {
        defaults.set(enabled, forKey: SharedPrefs.Key.connectionAlerts)
        connectionAlerts = enabled
    }

    func setPanicWipe(_ enabled: Bool) {
        defaults.set(enabled, forKey: SharedPrefs.Key.panicWipe)
        panicWipe = enabled
    }

    func setAccent(_ choice: Accent) {
        defaults.set(choice.rawValue, forKey: SharedPrefs.Key.accent)
        accent = choice
        AccentTheme.shared.accent = choice
    }

    func setDeveloperMode(_ enabled: Bool) {
        defaults.set(enabled, forKey: SharedPrefs.Key.developerMode)
        developerMode = enabled
    }

    func setSplitTunnelEnabled(_ enabled: Bool) {
        defaults.set(enabled, forKey: SharedPrefs.Key.splitTunnelEnabled)
        splitTunnelEnabled = enabled
    }

    // Callers validate first; this stores exactly what it is given.
    func setSplitTunnelRanges(_ ranges: [String]) {
        defaults.set(ranges, forKey: SharedPrefs.Key.splitTunnelRanges)
        splitTunnelRanges = ranges
    }

    // A failure to run the probe at all reads as a failed probe, with the reason as its detail.
    func testHubMethod(_ method: String) async -> HubTestResult {
        let core = self.core
        do {
            return try await CoreQueue.run { try core.testHubMethod(method) }
        } catch {
            Self.log.error("hub test failed to run: \(error.localizedDescription, privacy: .private)")
            return HubTestResult(method: method, ok: false, ms: 0, detail: error.localizedDescription)
        }
    }

    func hubStatus() async -> HubStatus {
        let core = self.core
        return await CoreQueue.perform { core.hubStatus() }
    }

    // After a panic wipe has erased the stored values, so nothing on screen outlives them.
    func resetToDefaults() {
        settings = AppSettings()
        loaded = false
        killSwitch = false
        darkTheme = nil
        autoConnect = true
        lockdown = false
        connectionAlerts = true
        panicWipe = false
        accent = .terra
        developerMode = false
        splitTunnelEnabled = false
        splitTunnelRanges = []
        multihopEnabled = false
        multihopEntryId = nil
        error = nil
        AccentTheme.shared.accent = .terra
    }

    func setMultihopEnabled(_ enabled: Bool) {
        defaults.set(enabled, forKey: SharedPrefs.Key.multihopEnabled)
        multihopEnabled = enabled
    }

    func setMultihopEntry(_ serverId: String?) {
        if let serverId, !serverId.isEmpty {
            defaults.set(serverId, forKey: SharedPrefs.Key.multihopEntryId)
        } else {
            defaults.removeObject(forKey: SharedPrefs.Key.multihopEntryId)
        }
        multihopEntryId = serverId.flatMap { $0.isEmpty ? nil : $0 }
    }

    private func edit(_ change: @escaping @Sendable (inout AppSettings) -> Void) async {
        let core = self.core
        await store { try core.editConfig(change) }
    }

    // The reply is the core's whole stored config, so it also counts as a load.
    private func store(_ write: @escaping @Sendable () throws -> AppSettings) async {
        do {
            settings = try await CoreQueue.run(on: CoreQueue.settings, write)
            loaded = true
            error = nil
        } catch {
            show(error, doing: "save")
        }
    }

    private func show(_ failure: Error, doing action: String) {
        Self.log.error("settings \(action, privacy: .public) failed: \(failure.localizedDescription, privacy: .private)")
        error = failure.localizedDescription
    }

    private static func dnsServers(in text: String) -> [String] {
        text.split(whereSeparator: { $0.isWhitespace || $0 == "," }).map(String.init)
    }
}
