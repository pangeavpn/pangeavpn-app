import Foundation
import Pangeacore
import Security
import os

final class KeychainSecretStore: NSObject, MobileSecretStoreProtocol {
    private static let service = "org.pangeavpn.core"
    private static let log = Logger(subsystem: "org.pangeavpn", category: "keychain")

    private func identity(_ key: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: Self.service,
            kSecAttrAccount as String: key,
            kSecAttrAccessGroup as String: AppGroup.identifier,
        ]
    }

    func get(_ key: String?) -> String {
        guard let key else { return "" }
        var query = identity(key)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound { return "" }
        guard status == errSecSuccess, let data = result as? Data else {
            Self.log.error("read failed: \(status)")
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
        let status = SecItemUpdate(
            identity(key) as CFDictionary, [kSecValueData as String: data] as CFDictionary)
        if status == errSecSuccess { return }
        guard status == errSecItemNotFound else {
            Self.log.error("update failed: \(status)")
            return
        }
        add(key, data)
    }

    private func add(_ key: String, _ data: Data) {
        var item = identity(key)
        item[kSecValueData as String] = data
        item[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        let status = SecItemAdd(item as CFDictionary, nil)
        if status != errSecSuccess { Self.log.error("add failed: \(status)") }
    }

    private func remove(_ key: String) {
        let status = SecItemDelete(identity(key) as CFDictionary)
        if status != errSecSuccess && status != errSecItemNotFound {
            Self.log.error("delete failed: \(status)")
        }
    }
}
