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
        toolUseID ?? UUID().uuidString
    }

    enum CodingKeys: String, CodingKey {
        case type, text
        case toolUseID = "tool_use_id"
        case toolName = "tool_name"
        case toolResult = "tool_result"
        case isError = "is_error"
    }
}
