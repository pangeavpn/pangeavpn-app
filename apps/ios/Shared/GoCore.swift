import Foundation
import Pangeacore

final class GoCore: CoreBridge {
    private static let lock = NSLock()
    private static var instance: GoCore?

    // First sink wins: later callers get the already-initialised core.
    static func shared(sink: ((TunnelStatus) -> Void)?) -> GoCore {
        lock.lock()
        defer { lock.unlock() }
        if let instance { return instance }
        MobileInit(
            KeychainSecretStore(),
            AlwaysProtect(),
            StatusAdapter(handler: sink),
            NetworkKeyAdapter(),
            AppGroup.containerURL.path)
        let core = GoCore()
        instance = core
        return core
    }

    private init() {}

    func restoreSession() throws -> Session {
        try decode(call { MobileRestoreSession($0) })
    }

    func login(token: String) throws -> Session {
        try decode(call { MobileLogin(token, $0) })
    }

    func logout() { MobileLogout() }

    func listServers() throws -> [Server] {
        try decodeList(call { MobileListServers($0) })
    }

    func subscription() throws -> Subscription? {
        let json = try call { MobileGetSubscription($0) }
        return isNull(json) ? nil : try decode(json)
    }

    func listDevices() throws -> [Device] {
        try decodeList(call { MobileListDevices($0) })
    }

    func removeDevice(id: String) throws {
        var err: NSError?
        let ok = MobileRemoveDevice(id, &err)
        try check(ok, err)
    }

    func prepare(serverId: String) throws -> TunnelConfig {
        try decode(call { MobilePrepare(serverId, $0) })
    }

    func start(tunFd: Int) throws {
        var err: NSError?
        let ok = MobileStart(tunFd, &err)
        try check(ok, err)
    }

    func stop() { MobileStop() }

    func state() -> TunnelStatus {
        let decoded: TunnelStatus? = try? decode(MobileState())
        return decoded ?? .disconnected
    }

    func config() throws -> AppSettings {
        try decode(call { MobileGetConfig($0) })
    }

    func setConfig(_ settings: AppSettings) throws -> AppSettings {
        guard let data = try? JSONEncoder().encode(settings),
              let raw = String(data: data, encoding: .utf8)
        else { throw CoreError.unexpectedResponse }
        return try decode(call { MobileSetConfig(raw, $0) })
    }

    func setHubMethod(_ method: String, enabled: Bool) throws -> AppSettings {
        try decode(call { MobileSetHubMethod(method, enabled, $0) })
    }

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

    private func isNull(_ json: String) -> Bool {
        json.trimmingCharacters(in: .whitespacesAndNewlines) == "null"
    }

    private func decode<T: Decodable>(_ json: String) throws -> T {
        do {
            return try JSONDecoder().decode(T.self, from: Data(json.utf8))
        } catch {
            throw CoreError.unexpectedResponse
        }
    }

    private func decodeList<T: Decodable>(_ json: String) throws -> [T] {
        isNull(json) ? [] : try decode(json)
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
