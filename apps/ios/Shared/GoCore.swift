import Foundation
import Pangeacore
import os

final class GoCore: CoreBridge {
    private static let lock = NSLock()
    private static var instance: GoCore?
    private static let log = Logger(subsystem: "org.pangeavpn", category: "core")

    // The same store Go was given, so a read-back sees exactly what Go persisted.
    private let secrets: KeychainSecretStore

    // First sink wins: later callers get the already-initialised core.
    static func shared(sink: ((TunnelStatus) -> Void)?) -> GoCore {
        lock.lock()
        defer { lock.unlock() }
        if let instance { return instance }
        let secrets = KeychainSecretStore()
        MobileInit(
            secrets,
            AlwaysProtect(),
            StatusAdapter(handler: sink),
            NetworkKeyAdapter(),
            AppGroup.containerURL.path)
        let core = GoCore(secrets: secrets)
        instance = core
        return core
    }

    private init(secrets: KeychainSecretStore) {
        self.secrets = secrets
    }

    func restoreSession() throws -> Session {
        try CoreJSON.decode(call { MobileRestoreSession($0) })
    }

    func login(token: String) throws -> Session {
        let session: Session = try CoreJSON.decode(call { MobileLogin(token, $0) })
        let stored = secrets.get(KeychainSecretStore.licenseKey)
        return try SignInCheck.confirm(session, storedLicense: stored, undo: { MobileLogout() })
    }

    func logout() { MobileLogout() }

    func listServers() throws -> [Server] {
        try CoreJSON.decodeList(call { MobileListServers($0) })
    }

    func subscription() throws -> Subscription? {
        try CoreJSON.decodeOptional(call { MobileGetSubscription($0) })
    }

    func listDevices() throws -> [Device] {
        try CoreJSON.decodeList(call { MobileListDevices($0) })
    }

    func removeDevice(id: String) throws {
        var err: NSError?
        let ok = MobileRemoveDevice(id, &err)
        try check(ok, err)
    }

    func prepare(serverId: String) throws -> TunnelConfig {
        try CoreJSON.decode(call { MobilePrepare(serverId, $0) })
    }

    func start(tunFd: Int) throws {
        var err: NSError?
        let ok = MobileStart(tunFd, &err)
        try check(ok, err)
    }

    func stop() { MobileStop() }

    func state() -> TunnelStatus {
        do {
            return try CoreJSON.decode(MobileState())
        } catch {
            Self.log.error("core status was unreadable")
            return .disconnected
        }
    }

    func config() throws -> AppSettings {
        try CoreJSON.decode(call { MobileGetConfig($0) })
    }

    func setConfig(_ settings: AppSettings) throws -> AppSettings {
        guard let data = try? JSONEncoder().encode(settings),
              let raw = String(data: data, encoding: .utf8)
        else { throw CoreError.unexpectedResponse }
        return try CoreJSON.decode(call { MobileSetConfig(raw, $0) })
    }

    func setHubMethod(_ method: String, enabled: Bool) throws -> AppSettings {
        try CoreJSON.decode(call { MobileSetHubMethod(method, enabled, $0) })
    }

    func reload() throws {
        var err: NSError?
        let ok = MobileReload(&err)
        try check(ok, err)
    }

    func networkChanged() { MobileNetworkChanged() }

    private func call(_ body: (NSErrorPointer) -> String) throws -> String {
        var err: NSError?
        let out = body(&err)
        if let err { throw CoreError.from(err) }
        return out
    }

    private func check(_ ok: Bool, _ err: NSError?) throws {
        if let err { throw CoreError.from(err) }
        if !ok { throw CoreError.from(nil) }
    }
}

private final class AlwaysProtect: NSObject, MobileSocketProtectorProtocol {
    func protect(_ fd: Int) -> Bool { true }
}

private final class StatusAdapter: NSObject, MobileStatusSinkProtocol {
    let handler: ((TunnelStatus) -> Void)?

    init(handler: ((TunnelStatus) -> Void)?) { self.handler = handler }

    func onStatus(_ statusJSON: String?) {
        guard let handler, let statusJSON,
              let status = try? JSONDecoder().decode(TunnelStatus.self, from: Data(statusJSON.utf8))
        else { return }
        handler(status)
    }
}

private final class NetworkKeyAdapter: NSObject, MobileNetworkKeyProviderProtocol {
    func networkKey() -> String { NetworkKey.current() }
}
