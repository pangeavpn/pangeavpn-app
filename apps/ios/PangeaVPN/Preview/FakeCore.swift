#if DEBUG
import Foundation

// Results are scripted before use; the lock covers the logs, which core worker threads write.
final class FakeCore: CoreBridge, @unchecked Sendable {
    var restoreResult: Result<Session, CoreError> = .failure(CoreError(message: "not authenticated"))
    var loginResult: Result<Session, CoreError> = .failure(CoreError(message: "not authenticated"))
    var serversResult: Result<[Server], CoreError> = .success([])
    var subscriptionResult: Result<Subscription?, CoreError> = .success(nil)
    var devicesResult: Result<[Device], CoreError> = .success([])
    var removeDeviceResult: Result<Void, CoreError> = .success(())
    var prepareResult: Result<TunnelConfig, CoreError> = .failure(CoreError(message: "no server"))
    var startResult: Result<Void, CoreError> = .success(())
    var configResult: Result<AppSettings, CoreError> = .success(AppSettings())
    // nil echoes the input back, as Go does for settings that need no correction.
    var setConfigResult: Result<AppSettings, CoreError>?
    var hubMethodResult: Result<AppSettings, CoreError> = .success(AppSettings())
    var currentState = TunnelStatus.disconnected

    private let lock = NSLock()
    private var entries: [String] = []
    private var saved: [AppSettings] = []

    var calls: [String] { lock.withLock { entries } }

    var savedSettings: [AppSettings] { lock.withLock { saved } }

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

    func subscription() throws -> Subscription? { try record("subscription", subscriptionResult) }

    func listDevices() throws -> [Device] { try record("listDevices", devicesResult) }

    func removeDevice(id: String) throws { try record("removeDevice(\(id))", removeDeviceResult) }

    func prepare(serverId: String) throws -> TunnelConfig {
        try record("prepare(\(serverId))", prepareResult)
    }

    func start(tunFd: Int) throws { try record("start(\(tunFd))", startResult) }

    func stop() { note("stop") }

    func state() -> TunnelStatus { currentState }

    func config() throws -> AppSettings { try record("config", configResult) }

    func setConfig(_ settings: AppSettings) throws -> AppSettings {
        lock.withLock { saved.append(settings) }
        return try record("setConfig", setConfigResult ?? .success(settings))
    }

    func setHubMethod(_ method: String, enabled: Bool) throws -> AppSettings {
        try record("setHubMethod(\(method),\(enabled))", hubMethodResult)
    }
}
#endif
