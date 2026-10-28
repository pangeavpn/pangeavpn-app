#if DEBUG
import Foundation

final class FakeCore: CoreBridge {
    var restoreResult: Result<Session, CoreError> = .failure(CoreError(message: "not authenticated"))
    var loginResult: Result<Session, CoreError> = .failure(CoreError(message: "not authenticated"))
    var serversResult: Result<[Server], CoreError> = .success([])
    var subscriptionResult: Result<Subscription?, CoreError> = .success(nil)
    var devicesResult: Result<[Device], CoreError> = .success([])
    var removeDeviceResult: Result<Void, CoreError> = .success(())
    var prepareResult: Result<TunnelConfig, CoreError> = .failure(CoreError(message: "no server"))
    var startResult: Result<Void, CoreError> = .success(())
    var configResult: Result<AppSettings, CoreError> = .success(AppSettings())
    var hubMethodResult: Result<AppSettings, CoreError> = .success(AppSettings())
    var currentState = TunnelStatus.disconnected

    private(set) var calls: [String] = []

    private func record<T>(_ name: String, _ result: Result<T, CoreError>) throws -> T {
        calls.append(name)
        return try result.get()
    }

    func restoreSession() throws -> Session { try record("restoreSession", restoreResult) }

    func login(token: String) throws -> Session { try record("login(\(token))", loginResult) }

    func logout() { calls.append("logout") }

    func listServers() throws -> [Server] { try record("listServers", serversResult) }

    func subscription() throws -> Subscription? { try record("subscription", subscriptionResult) }

    func listDevices() throws -> [Device] { try record("listDevices", devicesResult) }

    func removeDevice(id: String) throws { try record("removeDevice(\(id))", removeDeviceResult) }

    func prepare(serverId: String) throws -> TunnelConfig {
        try record("prepare(\(serverId))", prepareResult)
    }

    func start(tunFd: Int) throws { try record("start(\(tunFd))", startResult) }

    func stop() { calls.append("stop") }

    func state() -> TunnelStatus { currentState }

    func config() throws -> AppSettings { try record("config", configResult) }

    func setConfig(_ settings: AppSettings) throws -> AppSettings {
        try record("setConfig", .success(settings))
    }

    func setHubMethod(_ method: String, enabled: Bool) throws -> AppSettings {
        try record("setHubMethod(\(method),\(enabled))", hubMethodResult)
    }
}
#endif
