import Foundation

struct CoreError: LocalizedError, Equatable {
    let message: String

    var errorDescription: String? { message }

    static let unexpectedResponse = CoreError(message: "Unexpected response from core")

    static func from(_ error: NSError?) -> CoreError {
        guard let error else { return CoreError(message: "Unknown error") }
        return CoreError(message: error.localizedDescription)
    }
}

// Sendable because the app runs core calls on worker threads; Go guards its own state.
protocol CoreBridge: AnyObject, Sendable {
    func restoreSession() throws -> Session
    func login(token: String) throws -> Session
    func logout()
    func listServers() throws -> [Server]
    func subscription() throws -> Subscription?
    func listDevices() throws -> [Device]
    func removeDevice(id: String) throws
    func prepare(serverId: String) throws -> TunnelConfig
    func start(tunFd: Int) throws
    func stop()
    func state() -> TunnelStatus
    func config() throws -> AppSettings
    func setConfig(_ settings: AppSettings) throws -> AppSettings
    func setHubMethod(_ method: String, enabled: Bool) throws -> AppSettings
}
