import Foundation

struct ConnectionInfo: Codable, Equatable, Sendable {
    let host: String
    let port: Int
    let token: String

    /// Decode from base64-encoded JSON (QR code format)
    static func decode(from base64String: String) -> ConnectionInfo? {
        // Trim whitespace and newlines
        let trimmed = base64String.trimmingCharacters(in: .whitespacesAndNewlines)

        guard let data = Data(base64Encoded: trimmed) else {
            print("QR decode: invalid base64: \(trimmed.prefix(50))...")
            return nil
        }

        do {
            let info = try JSONDecoder().decode(ConnectionInfo.self, from: data)
            print("QR decode: success - host=\(info.host), port=\(info.port)")
            return info
        } catch {
            print("QR decode: JSON error: \(error)")
            if let jsonString = String(data: data, encoding: .utf8) {
                print("QR decode: raw JSON: \(jsonString)")
            }
            return nil
        }
    }

    /// Encode to base64 JSON (for debugging/display)
    func encode() -> String? {
        guard let data = try? JSONEncoder().encode(self) else {
            return nil
        }
        return data.base64EncodedString()
    }
}
