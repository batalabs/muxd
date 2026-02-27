import Foundation

struct TranscriptMessage: Codable, Identifiable, Sendable {
    let role: String
    let content: String
    let blocks: [ContentBlock]?

    enum CodingKeys: String, CodingKey {
        case role = "Role"
        case content = "Content"
        case blocks = "Blocks"
    }

    var id: String {
        // Generate a stable ID from content hash
        "\(role)_\(content.hashValue)"
    }

    var textContent: String {
        if let blocks = blocks, !blocks.isEmpty {
            return blocks
                .filter { $0.type == "text" }
                .compactMap { $0.text }
                .joined(separator: "\n")
        }
        return content
    }

    var isUser: Bool {
        role == "user"
    }

    var isAssistant: Bool {
        role == "assistant"
    }

    var toolUseBlocks: [ContentBlock] {
        blocks?.filter { $0.type == "tool_use" } ?? []
    }

    var toolResultBlocks: [ContentBlock] {
        blocks?.filter { $0.type == "tool_result" } ?? []
    }
}

struct ContentBlock: Codable, Identifiable, Sendable {
    let type: String
    let text: String?
    let toolUseID: String?
    let toolName: String?
    let toolResult: String?
    let isError: Bool?

    var id: String {
        // Use toolUseID if available, otherwise generate stable ID from content
        if let toolID = toolUseID {
            return toolID
        }
        // Generate stable ID from type and text content
        let textHash = (text ?? "").hashValue
        return "\(type)_\(textHash)"
    }

    // Check if tool result contains base64 image data
    var isImageResult: Bool {
        guard let result = toolResult else { return false }
        return result.hasPrefix("data:image/") ||
               result.hasPrefix("iVBOR") || // PNG base64
               result.hasPrefix("/9j/")     // JPEG base64
    }

    // Extract base64 image data
    var imageData: Data? {
        guard let result = toolResult else { return nil }

        // Handle data URI format: data:image/png;base64,xxxxx
        if result.hasPrefix("data:image/") {
            if let commaIndex = result.firstIndex(of: ",") {
                let base64String = String(result[result.index(after: commaIndex)...])
                return Data(base64Encoded: base64String)
            }
        }

        // Try direct base64 decode
        return Data(base64Encoded: result)
    }

    enum CodingKeys: String, CodingKey {
        case type, text
        case toolUseID = "tool_use_id"
        case toolName = "tool_name"
        case toolResult = "tool_result"
        case isError = "is_error"
    }
}
