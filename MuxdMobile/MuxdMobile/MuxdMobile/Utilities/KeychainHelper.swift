import Foundation
import Security

enum KeychainHelper {
    private static let service = "com.muxd.mobile"
    private static let connectionsKey = "saved_connections"
    private static let activeConnectionKey = "active_connection_id"
    private static let legacyConnectionKey = "connection_info"

    // MARK: - Multiple Connections

    static func saveConnections(_ connections: [ConnectionInfo]) {
        guard let data = try? JSONEncoder().encode(connections) else { return }
        saveData(data, forKey: connectionsKey)
    }

    static func loadConnections() -> [ConnectionInfo] {
        if let data = loadData(forKey: connectionsKey),
           let connections = try? JSONDecoder().decode([ConnectionInfo].self, from: data) {
            return connections
        }

        // Migrate from legacy single connection format
        if let data = loadData(forKey: legacyConnectionKey),
           let legacy = try? JSONDecoder().decode(ConnectionInfo.self, from: data) {
            saveConnections([legacy])
            deleteData(forKey: legacyConnectionKey)
            return [legacy]
        }

        return []
    }

    static func addConnection(_ connection: ConnectionInfo) {
        var connections = loadConnections()
        connections.removeAll { $0.id == connection.id }
        connections.insert(connection, at: 0)
        saveConnections(connections)
    }

    static func removeConnection(id: String) {
        var connections = loadConnections()
        connections.removeAll { $0.id == id }
        saveConnections(connections)

        if loadActiveConnectionID() == id {
            deleteActiveConnectionID()
        }
    }

    static func updateConnectionName(id: String, name: String) {
        var connections = loadConnections()
        if let index = connections.firstIndex(where: { $0.id == id }) {
            connections[index].name = name
            saveConnections(connections)
        }
    }

    // MARK: - Active Connection

    static func saveActiveConnectionID(_ id: String) {
        guard let data = id.data(using: .utf8) else { return }
        saveData(data, forKey: activeConnectionKey)
    }

    static func loadActiveConnectionID() -> String? {
        guard let data = loadData(forKey: activeConnectionKey),
              let id = String(data: data, encoding: .utf8) else {
            return nil
        }
        return id
    }

    static func deleteActiveConnectionID() {
        deleteData(forKey: activeConnectionKey)
    }

    // MARK: - Legacy Compatibility

    static func save(connectionInfo: ConnectionInfo) {
        addConnection(connectionInfo)
        saveActiveConnectionID(connectionInfo.id)
    }

    static func loadConnectionInfo() -> ConnectionInfo? {
        // Only restore if there's an explicit active connection ID
        guard let activeID = loadActiveConnectionID(),
              let connection = loadConnections().first(where: { $0.id == activeID }) else {
            return nil
        }
        return connection
    }

    static func deleteConnectionInfo() {
        deleteActiveConnectionID()
    }

    // MARK: - Private Helpers

    private static func saveData(_ data: Data, forKey key: String) {
        let deleteQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key
        ]
        SecItemDelete(deleteQuery as CFDictionary)

        let addQuery: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        ]
        SecItemAdd(addQuery as CFDictionary, nil)
    }

    private static func loadData(forKey key: String) -> Data? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne
        ]

        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        guard status == errSecSuccess, let data = result as? Data else {
            return nil
        }
        return data
    }

    private static func deleteData(forKey key: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key
        ]
        SecItemDelete(query as CFDictionary)
    }
}
