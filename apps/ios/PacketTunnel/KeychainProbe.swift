import Foundation
import Security

enum KeychainProbe {
    // Asks for the data because item attributes stay readable while the device is locked.
    static func lockedError() -> TunnelStartError? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: KeychainSecretStore.service,
            kSecAttrAccessGroup as String: AppGroup.identifier,
            kSecMatchLimit as String: kSecMatchLimitOne,
            kSecReturnData as String: true,
        ]
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        return TunnelStart.keychainError(status: status)
    }
}
