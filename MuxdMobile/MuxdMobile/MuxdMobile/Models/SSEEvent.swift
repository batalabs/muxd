import Foundation

enum SSEEventType: String {
    case delta
    case toolStart = "tool_start"
    case toolDone = "tool_done"
    case streamDone = "stream_done"
    case askUser = "ask_user"
    case turnDone = "turn_done"
    case error
    case compacted
    case titled
    case retrying
}

struct SSEEvent: Sendable {
    let type: SSEEventType
    var deltaText: String?
    var toolUseID: String?
    var toolName: String?
    var toolInputSummary: String?  // e.g., file path for Read, command for Bash
    var toolResult: String?
    var toolIsError: Bool?
    var inputTokens: Int?
    var outputTokens: Int?
    var cacheCreationInputTokens: Int?
    var cacheReadInputTokens: Int?
    var stopReason: String?
    var askID: String?
    var askPrompt: String?
    var errorMsg: String?
    var title: String?
    var tags: String?
    var retryAttempt: Int?
    var retryWaitMs: Int?
    var retryMessage: String?

    init(type: SSEEventType) {
        self.type = type
    }

    static func parse(eventType: String, data: Data) -> SSEEvent? {
        guard let type = SSEEventType(rawValue: eventType) else {
            return nil
        }

        guard let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return nil
        }

        var event = SSEEvent(type: type)

        switch type {
        case .delta:
            event.deltaText = json["text"] as? String

        case .toolStart:
            event.toolUseID = json["tool_use_id"] as? String
            event.toolName = json["tool_name"] as? String
            // Extract a human-readable summary from tool_input
            if let toolInput = json["tool_input"] as? [String: Any] {
                event.toolInputSummary = Self.extractToolInputSummary(toolName: event.toolName, input: toolInput)
            }

        case .toolDone:
            event.toolUseID = json["tool_use_id"] as? String
            event.toolName = json["tool_name"] as? String
            event.toolResult = json["result"] as? String
            event.toolIsError = json["is_error"] as? Bool

        case .streamDone:
            event.inputTokens = json["input_tokens"] as? Int
            event.outputTokens = json["output_tokens"] as? Int
            event.cacheCreationInputTokens = json["cache_creation_input_tokens"] as? Int
            event.cacheReadInputTokens = json["cache_read_input_tokens"] as? Int
            event.stopReason = json["stop_reason"] as? String

        case .askUser:
            event.askID = json["ask_id"] as? String
            event.askPrompt = json["prompt"] as? String

        case .turnDone:
            event.stopReason = json["stop_reason"] as? String

        case .error:
            event.errorMsg = json["error"] as? String

        case .compacted:
            break // No fields

        case .titled:
            event.title = json["title"] as? String
            event.tags = json["tags"] as? String

        case .retrying:
            event.retryAttempt = json["attempt"] as? Int
            event.retryWaitMs = json["wait_ms"] as? Int
            event.retryMessage = json["message"] as? String
        }

        return event
    }

    /// Extracts a human-readable summary from tool input based on tool type
    private static func extractToolInputSummary(toolName: String?, input: [String: Any]) -> String? {
        guard let name = toolName else { return nil }

        switch name {
        case "file_read":
            return input["path"] as? String
        case "file_write", "file_edit":
            return input["path"] as? String
        case "bash":
            return input["command"] as? String
        case "grep":
            return input["pattern"] as? String
        case "list_files":
            return input["path"] as? String
        default:
            return nil
        }
    }
}
