import Foundation

struct CoreError: LocalizedError, Equatable {
    let message: String

    var errorDescription: String? { message }

    static let unexpectedResponse = CoreError(message: "Unexpected response from core")

    static let signInNotSaved = CoreError(message: String(
        localized: "error_signin_not_saved",
        defaultValue: "Could not save your sign-in on this device."))

    private static let goPrefix = "mobile: "

    static func from(_ error: NSError?) -> CoreError {
        guard let error else { return CoreError(message: "Unknown error") }
        let text = error.localizedDescription
        return CoreError(message: text.hasPrefix(goPrefix) ? String(text.dropFirst(goPrefix.count)) : text)
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
    func reload() throws
    func networkChanged()
}

// Go marshals a nil slice or pointer as a bare null.
enum CoreJSON {
    static func isNull(_ json: String) -> Bool {
        json.trimmingCharacters(in: .whitespacesAndNewlines) == "null"
    }

    static func decode<T: Decodable>(_ json: String) throws -> T {
        do {
            return try JSONDecoder().decode(T.self, from: Data(json.utf8))
        } catch {
            throw CoreError.unexpectedResponse
        }
    }

    static func decodeList<T: Decodable>(_ json: String) throws -> [T] {
        if isNull(json) { return [] }
        return try decode(json)
    }

    static func decodeOptional<T: Decodable>(_ json: String) throws -> T? {
        if isNull(json) { return nil }
        let value: T = try decode(json)
        return value
    }
}

// Go's SecretStore.Set cannot report a failure, so a lost Keychain write only shows on read-back.
enum SignInCheck {
    static func confirm(_ session: Session, storedLicense: String, undo: () -> Void) throws -> Session {
        guard storedLicense.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return session }
        undo()
        throw CoreError.signInNotSaved
    }
}

extension Error {
    // Safe to log in public, unlike the message, which can carry hub and node addresses.
    var logCode: String {
        let ns = self as NSError
        return "\(ns.domain) \(ns.code)"
    }
}
