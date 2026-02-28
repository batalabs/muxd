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

    /// Returns tool_result blocks with toolInputSummary populated from matching tool_use blocks
    func toolResultBlocksWithInput(from allMessages: [TranscriptMessage]) -> [ContentBlock] {
        let resultBlocks = toolResultBlocks
        guard !resultBlocks.isEmpty else { return [] }

        // Build a lookup of toolUseID -> toolInputSummary from all tool_use blocks
        var inputSummaryByID: [String: String] = [:]
        for msg in allMessages {
            for block in msg.toolUseBlocks {
                if let id = block.toolUseID, let summary = block.toolInputSummary {
                    inputSummaryByID[id] = summary
                }
            }
        }

        // Enrich tool_result blocks with the input summary
        return resultBlocks.map { block in
            var enriched = block
            if let id = block.toolUseID, let summary = inputSummaryByID[id] {
                enriched.toolInputSummary = summary
            }
            return enriched
        }
    }
}

struct ContentBlock: Codable, Identifiable, Sendable {
    let type: String
    let text: String?
    let toolUseID: String?
    let toolName: String?
    var toolInputSummary: String?  // e.g., file path for Read, command for Bash
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
        case toolInput = "tool_input"
        case toolResult = "tool_result"
        case isError = "is_error"
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        type = try container.decode(String.self, forKey: .type)
        text = try container.decodeIfPresent(String.self, forKey: .text)
        toolUseID = try container.decodeIfPresent(String.self, forKey: .toolUseID)
        toolName = try container.decodeIfPresent(String.self, forKey: .toolName)
        toolResult = try container.decodeIfPresent(String.self, forKey: .toolResult)
        isError = try container.decodeIfPresent(Bool.self, forKey: .isError)

        // Parse tool_input and extract summary
        if let toolInput = try container.decodeIfPresent([String: AnyCodable].self, forKey: .toolInput) {
            toolInputSummary = Self.extractToolInputSummary(toolName: toolName, input: toolInput)
        } else {
            toolInputSummary = nil
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(type, forKey: .type)
        try container.encodeIfPresent(text, forKey: .text)
        try container.encodeIfPresent(toolUseID, forKey: .toolUseID)
        try container.encodeIfPresent(toolName, forKey: .toolName)
        try container.encodeIfPresent(toolResult, forKey: .toolResult)
        try container.encodeIfPresent(isError, forKey: .isError)
        // Note: toolInputSummary is derived, not encoded
    }

    /// Extracts a human-readable summary from tool input based on tool type
    private static func extractToolInputSummary(toolName: String?, input: [String: AnyCodable]) -> String? {
        guard let name = toolName else { return nil }

        switch name {
        case "file_read":
            return input["path"]?.value as? String
        case "file_write", "file_edit":
            return input["path"]?.value as? String
        case "bash":
            return input["command"]?.value as? String
        case "grep":
            return input["pattern"]?.value as? String
        case "list_files":
            return input["path"]?.value as? String
        default:
            return nil
        }
    }
}

/// Helper type to decode arbitrary JSON values
struct AnyCodable: Codable, @unchecked Sendable {
    let value: Any

    init(_ value: Any) {
        self.value = value
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let string = try? container.decode(String.self) {
            value = string
        } else if let int = try? container.decode(Int.self) {
            value = int
        } else if let double = try? container.decode(Double.self) {
            value = double
        } else if let bool = try? container.decode(Bool.self) {
            value = bool
        } else if let array = try? container.decode([AnyCodable].self) {
            value = array.map { $0.value }
        } else if let dict = try? container.decode([String: AnyCodable].self) {
            value = dict.mapValues { $0.value }
        } else {
            value = NSNull()
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        if let string = value as? String {
            try container.encode(string)
        } else if let int = value as? Int {
            try container.encode(int)
        } else if let double = value as? Double {
            try container.encode(double)
        } else if let bool = value as? Bool {
            try container.encode(bool)
        } else {
            try container.encodeNil()
        }
    }
}
