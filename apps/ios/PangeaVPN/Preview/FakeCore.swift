#if DEBUG
import Foundation

// Results are scripted before use; the lock covers what core worker threads read and write.
final class FakeCore: CoreBridge, @unchecked Sendable {
    var restoreResult: Result<Session, CoreError> = .failure(CoreError(message: "not authenticated"))
    var loginResult: Result<Session, CoreError> = .failure(CoreError(message: "not authenticated"))
    var serversResult: Result<[Server], CoreError> = .success([])
    var subscriptionResult: Result<Subscription?, CoreError> = .success(nil)
    var devicesResult: Result<[Device], CoreError> = .success([])
    var removeDeviceResult: Result<Void, CoreError> = .success(())
    // nil echoes the requested name back trimmed, as the hub does.
    var renameResult: Result<String, CoreError>?
    var accountNumberValue = ""
    var logsValue = ""
    var diagnosticsResult: Result<String, CoreError> = .success("R-TEST1")
    var hubTestResults: [String: Result<HubTestResult, CoreError>] = [:]
    var hubStatusValue = HubStatus.none
    var deregisterResult: Result<Void, CoreError> = .success(())
    var wipeResult: Result<Void, CoreError> = .success(())
    var multihopEntriesValue: [String] = []
    var prepareResult: Result<TunnelConfig, CoreError> = .failure(CoreError(message: "no server"))
    var startResult: Result<Void, CoreError> = .success(())
    var reloadResult: Result<Void, CoreError> = .success(())
    // nil echoes the input back, as Go does for settings that need no correction.
    var setConfigResult: Result<AppSettings, CoreError>?
    var hubMethodResult: Result<AppSettings, CoreError> = .success(AppSettings())
    var currentState = TunnelStatus.disconnected
    // Widens the read-then-write window, so an unserialized edit would lose a change.
    var configDelay: TimeInterval = 0
    // Holds a subscription lookup after it is noted, so a test can act while it is in flight.
    var subscriptionDelay: TimeInterval = 0

    private let lock = NSLock()
    private var entries: [String] = []
    private var sentExtraLogs: String?

    var lastExtraLogs: String? { lock.withLock { sentExtraLogs } }
    private var saved: [AppSettings] = []
    private var stored: Result<AppSettings, CoreError> = .success(AppSettings())

    var calls: [String] { lock.withLock { entries } }

    var savedSettings: [AppSettings] { lock.withLock { saved } }

    // As in Go, a successful save is what the next config() returns.
    var configResult: Result<AppSettings, CoreError> {
        get { lock.withLock { stored } }
        set { lock.withLock { stored = newValue } }
    }

    func note(_ entry: String) {
        lock.withLock { entries.append(entry) }
    }

    private func record<T>(_ name: String, _ result: Result<T, CoreError>) throws -> T {
        note(name)
        return try result.get()
    }

    func restoreSession() throws -> Session { try record("restoreSession", restoreResult) }

    func login(token: String) throws -> Session { try record("login(\(token))", loginResult) }

    func logout() { note("logout") }

    func listServers() throws -> [Server] { try record("listServers", serversResult) }

    func subscription() throws -> Subscription? {
        let result = subscriptionResult
        note("subscription")
        if subscriptionDelay > 0 { Thread.sleep(forTimeInterval: subscriptionDelay) }
        return try result.get()
    }

    func listDevices() throws -> [Device] { try record("listDevices", devicesResult) }

    func removeDevice(id: String) throws { try record("removeDevice(\(id))", removeDeviceResult) }

    func renameDevice(id: String, name: String) throws -> String {
        let echoed = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return try record("renameDevice(\(id),\(name))", renameResult ?? .success(echoed))
    }

    func accountNumber() -> String {
        note("accountNumber")
        return accountNumberValue
    }

    func logs() -> String {
        note("logs")
        return logsValue
    }

    func sendDiagnostics(note text: String, extraLogs: String) throws -> String {
        lock.withLock { sentExtraLogs = extraLogs }
        return try record("sendDiagnostics(\(text))", diagnosticsResult)
    }

    func testHubMethod(_ method: String) throws -> HubTestResult {
        let result = hubTestResults[method] ?? .success(HubTestResult(ok: true, ms: 120))
        return try record("testHubMethod(\(method))", result)
    }

    func hubStatus() -> HubStatus { hubStatusValue }

    func deregister() throws { try record("deregister", deregisterResult) }

    func wipe() throws { try record("wipe", wipeResult) }

    func setClientInfo(appVersion: String, osVersion: String) {
        note("setClientInfo(\(appVersion),\(osVersion))")
    }

    func prepareVia(exitId: String, entryId: String) throws -> TunnelConfig {
        try record("prepareVia(\(exitId),\(entryId))", prepareResult)
    }

    func multihopEntries(exitId: String) -> [String] {
        note("multihopEntries(\(exitId))")
        return multihopEntriesValue
    }

    func stopForSwitch() { note("stopForSwitch") }

    func prepare(serverId: String) throws -> TunnelConfig {
        try record("prepare(\(serverId))", prepareResult)
    }

    func start(tunFd: Int) throws { try record("start(\(tunFd))", startResult) }

    func stop() { note("stop") }

    func state() -> TunnelStatus { currentState }

    func config() throws -> AppSettings {
        let result = configResult
        if configDelay > 0 { Thread.sleep(forTimeInterval: configDelay) }
        return try record("config", result)
    }

    func setConfig(_ settings: AppSettings) throws -> AppSettings {
        lock.withLock { saved.append(settings) }
        let result = setConfigResult ?? .success(settings)
        if case .success = result { configResult = result }
        return try record("setConfig", result)
    }

    func setHubMethod(_ method: String, enabled: Bool) throws -> AppSettings {
        try record("setHubMethod(\(method),\(enabled))", hubMethodResult)
    }

    func reload() throws { try record("reload", reloadResult) }

    func networkChanged() { note("networkChanged") }
}
#endif
