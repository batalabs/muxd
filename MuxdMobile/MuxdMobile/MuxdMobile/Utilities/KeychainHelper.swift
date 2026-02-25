import Foundation
import Security

enum KeychainHelper {
    private static let service = "com.muxd.mobile"
    private static let connectionKey = "connection_info"

    static func save(connectionInfo: ConnectionInfo) {
        guard let data = try? JSONEncoder().encode(connectionInfo) else { return }

        // Delete existing item first
        let deleteQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: connectionKey
        ]
        SecItemDelete(deleteQuery as CFDictionary)

        // Add new item
        let addQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: connectionKey,
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        ]
        SecItemAdd(addQuery as CFDictionary, nil)
    }

    static func loadConnectionInfo() -> ConnectionInfo? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: connectionKey,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        guard status == errSecSuccess else {
            print("Keychain: no saved data (status: \(status))")
            return nil
        }

        guard let data = result as? Data else {
            print("Keychain: result is not Data")
            return nil
        }

        do {
            let info = try JSONDecoder().decode(ConnectionInfo.self, from: data)
            print("Keychain: loaded connection to \(info.host):\(info.port)")
            return info
        } catch {
            print("Keychain: decode error: \(error)")
            return nil
        }
    }

    static func deleteConnectionInfo() {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: connectionKey
        ]
        SecItemDelete(query as CFDictionary)
    }
}
