import Foundation
import Observation
import os

@MainActor
@Observable
final class SettingsStore {
    static let killSwitchKey = "killSwitch"
    static let darkThemeKey = "darkTheme"

    private(set) var settings = AppSettings()
    private(set) var loaded = false
    // Applied by the app on the next connect, so it lives in defaults rather than the core.
    private(set) var killSwitch = false
    // nil follows the system appearance.
    private(set) var darkTheme: Bool?
    var error: String?

    private let core: any CoreBridge
    private let defaults: UserDefaults

    private static let log = Logger(subsystem: "org.pangeavpn", category: "settings")

    init(core: any CoreBridge, defaults: UserDefaults = AppGroup.defaults) {
        self.core = core
        self.defaults = defaults
        killSwitch = defaults.bool(forKey: Self.killSwitchKey)
        darkTheme = defaults.object(forKey: Self.darkThemeKey) as? Bool
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
