import Foundation

struct ConnectionInfo: Codable, Equatable, Sendable, Identifiable, Hashable {
    let id: String
    let host: String
    let port: Int
    let token: String
    var name: String

    init(id: String = UUID().uuidString, host: String, port: Int, token: String, name: String? = nil) {
        self.id = id
        self.host = host
        self.port = port
        self.token = token
        self.name = name ?? host
    }

    /// Decode from base64-encoded JSON (QR code format)
    static func decode(from base64String: String) -> ConnectionInfo? {
        let trimmed = base64String.trimmingCharacters(in: .whitespacesAndNewlines)

        guard let data = Data(base64Encoded: trimmed) else {
            return nil
        }

        // Try new format first (has id/name)
        if let info = try? JSONDecoder().decode(ConnectionInfo.self, from: data) {
            return info
        }

        // Fallback: decode legacy format (host, port, token only)
        struct LegacyConnectionInfo: Codable {
            let host: String
            let port: Int
            let token: String
        }

        guard let legacy = try? JSONDecoder().decode(LegacyConnectionInfo.self, from: data) else {
            return nil
        }

        return ConnectionInfo(host: legacy.host, port: legacy.port, token: legacy.token)
    }

    /// Encode to base64 JSON (for debugging/display)
    func encode() -> String? {
        guard let data = try? JSONEncoder().encode(self) else {
            return nil
        }
        return data.base64EncodedString()
    }

    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
    }

    static func == (lhs: ConnectionInfo, rhs: ConnectionInfo) -> Bool {
        lhs.id == rhs.id
    }
}
