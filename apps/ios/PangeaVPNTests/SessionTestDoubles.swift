import Foundation
@testable import PangeaVPN

final class FakeAuthenticator: DeviceAuthenticating, @unchecked Sendable {
    private let outcome: DeviceAuthOutcome
    private let core: FakeCore?
    private let lock = NSLock()
    private var asked: [String] = []

    // Notes "authenticate" into the core's calls, so a test can check it came first.
    init(_ outcome: DeviceAuthOutcome, core: FakeCore? = nil) {
        self.outcome = outcome
        self.core = core
    }

    var reasons: [String] { lock.withLock { asked } }

    func authenticate(reason: String) async -> DeviceAuthOutcome {
        lock.withLock { asked.append(reason) }
        core?.note("authenticate")
        return outcome
    }
}

// Forwards to a FakeCore, but deregister and wipe block until released, to drive the timeout paths.
final class GatedCore: CoreBridge, @unchecked Sendable {
    let fake: FakeCore
    private let gate = DispatchSemaphore(value: 0)
    // Bounds the block, so a broken timeout fails the test instead of hanging it.
    private let longest: TimeInterval = 5

    init(_ fake: FakeCore) { self.fake = fake }

    func release() { gate.signal() }

    private func hold() { _ = gate.wait(timeout: .now() + longest) }

    func deregister() throws {
        hold()
        try fake.deregister()
    }

    func wipe() throws {
        hold()
        try fake.wipe()
    }

    func restoreSession() throws -> Session { try fake.restoreSession() }
    func login(token: String) throws -> Session { try fake.login(token: token) }
    func logout() { fake.logout() }
    func listServers() throws -> [Server] { try fake.listServers() }
    func subscription() throws -> Subscription? { try fake.subscription() }
    func listDevices() throws -> [Device] { try fake.listDevices() }
    func removeDevice(id: String) throws { try fake.removeDevice(id: id) }
    func renameDevice(id: String, name: String) throws -> String { try fake.renameDevice(id: id, name: name) }
    func accountNumber() -> String { fake.accountNumber() }
    func logs() -> String { fake.logs() }
    func sendDiagnostics(note: String, extraLogs: String) throws -> String {
        try fake.sendDiagnostics(note: note, extraLogs: extraLogs)
    }
    func testHubMethod(_ method: String) throws -> HubTestResult { try fake.testHubMethod(method) }
    func hubStatus() -> HubStatus { fake.hubStatus() }
    func setClientInfo(appVersion: String, osVersion: String) {
        fake.setClientInfo(appVersion: appVersion, osVersion: osVersion)
    }
    func prepare(serverId: String) throws -> TunnelConfig { try fake.prepare(serverId: serverId) }
    func prepareVia(exitId: String, entryId: String) throws -> TunnelConfig {
        try fake.prepareVia(exitId: exitId, entryId: entryId)
    }
    func multihopEntries(exitId: String) -> [String] { fake.multihopEntries(exitId: exitId) }
    func start(tunFd: Int) throws { try fake.start(tunFd: tunFd) }
    func stop() { fake.stop() }
    func stopForSwitch() { fake.stopForSwitch() }
    func state() -> TunnelStatus { fake.state() }
    func config() throws -> AppSettings { try fake.config() }
    func setConfig(_ settings: AppSettings) throws -> AppSettings { try fake.setConfig(settings) }
    func setHubMethod(_ method: String, enabled: Bool) throws -> AppSettings {
        try fake.setHubMethod(method, enabled: enabled)
    }
    func reload() throws { try fake.reload() }
    func networkChanged() { fake.networkChanged() }
}

// Throwaway defaults and container for a wipe, so tests never clear the real ones.
struct WipeFixture {
    let group: (defaults: UserDefaults, suite: String)
    let standard: (defaults: UserDefaults, suite: String)
    let container: URL

    init() throws {
        group = try makeTestDefaults()
        standard = try makeTestDefaults()
        container = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try FileManager.default.createDirectory(at: container, withIntermediateDirectories: true)
    }

    func wiper(keychainCleared: Bool = true) -> PanicWiper {
        PanicWiper(
            domains: [
                DefaultsDomain(defaults: group.defaults, name: group.suite),
                DefaultsDomain(defaults: standard.defaults, name: standard.suite),
            ],
            markerDefaults: standard.defaults,
            container: container,
            removeKeychain: { keychainCleared })
    }

    func write(_ relativePath: String) throws {
        let url = container.appendingPathComponent(relativePath)
        try FileManager.default.createDirectory(
            at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data("x".utf8).write(to: url)
    }

    func exists(_ relativePath: String) -> Bool {
        FileManager.default.fileExists(atPath: container.appendingPathComponent(relativePath).path)
    }

    func cleanUp() {
        group.defaults.removePersistentDomain(forName: group.suite)
        standard.defaults.removePersistentDomain(forName: standard.suite)
        try? FileManager.default.removeItem(at: container)
    }
}
