import Foundation
import Pangeacore
import Security
import os

// Holds no state of its own, and the Security calls it makes are thread-safe.
final class KeychainSecretStore: NSObject, MobileSecretStoreProtocol, @unchecked Sendable {
    // Must match keyLicense in daemon/mobile/mobile.go.
    static let licenseKey = "licenseKey"

    static let service = "org.pangeavpn.core"
    private static let log = Logger(subsystem: "org.pangeavpn", category: "keychain")

    private static func query(account: String?) -> [String: Any] {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccessGroup as String: AppGroup.identifier,
        ]
        if let account { query[kSecAttrAccount as String] = account }
        return query
    }

    // Deletes every key this store holds; true when nothing is left behind.
    static func removeAll() -> Bool {
        let status = SecItemDelete(query(account: nil) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            log.error("wipe failed: \(status, privacy: .public)")
            return false
        }
        return true
    }

    func get(_ key: String?) -> String {
        guard let key else { return "" }
        var query = Self.query(account: key)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return "" }
        guard status == errSecSuccess, let data = result as? Data else {
            Self.log.error("read failed: \(status, privacy: .public)")
            return ""
        }
        return String(data: data, encoding: .utf8) ?? ""
    }

    func set(_ key: String?, value: String?) {
        guard let key else { return }
        guard let value, !value.isEmpty else {
            remove(key)
            return
        }
        let data = Data(value.utf8)
        let status = update(key, data)
        if status == errSecSuccess { return }
        guard status == errSecItemNotFound else {
            Self.log.error("update failed: \(status, privacy: .public)")
            return
        }
        add(key, data)
    }

    private func update(_ key: String, _ data: Data) -> OSStatus {
        SecItemUpdate(
            Self.query(account: key) as CFDictionary, [kSecValueData as String: data] as CFDictionary)
    }

    // The app and the extension can both add the same key, so the slower one updates instead.
    private func add(_ key: String, _ data: Data) {
        var item = Self.query(account: key)
        item[kSecValueData as String] = data
        item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let status = SecItemAdd(item as CFDictionary, nil)
        switch status {
        case errSecSuccess:
            return
        case errSecDuplicateItem:
            let retry = update(key, data)
            if retry != errSecSuccess { Self.log.error("update after duplicate failed: \(retry, privacy: .public)") }
        default:
            Self.log.error("add failed: \(status, privacy: .public)")
        }
    }

    private func remove(_ key: String) {
        let status = SecItemDelete(Self.query(account: key) as CFDictionary)
        if status != errSecSuccess && status != errSecItemNotFound {
            Self.log.error("delete failed: \(status, privacy: .public)")
        }
    }
}
