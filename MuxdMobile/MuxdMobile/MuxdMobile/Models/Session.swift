import Foundation

struct Session: Codable, Identifiable, Hashable, Equatable, Sendable {
    let id: String
    let projectPath: String
    var title: String
    let model: String
    var totalTokens: Int
    var inputTokens: Int
    var outputTokens: Int
    var messageCount: Int
    let parentSessionID: String?
    let branchPoint: Int?
    var tags: String?
    let createdAt: Date
    var updatedAt: Date

    // Custom Hashable - only use id for navigation path matching
    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
    }

    static func == (lhs: Session, rhs: Session) -> Bool {
        lhs.id == rhs.id
    }

    enum CodingKeys: String, CodingKey {
        case id
        case projectPath = "project_path"
        case title, model
        case totalTokens = "total_tokens"
        case inputTokens = "input_tokens"
        case outputTokens = "output_tokens"
        case messageCount = "message_count"
        case parentSessionID = "parent_session_id"
        case branchPoint = "branch_point"
        case tags
        case createdAt = "created_at"
        case updatedAt = "updated_at"
    }

    var shortID: String {
        String(id.prefix(8))
    }

    var displayTitle: String {
        title.isEmpty ? "Untitled Session" : title
    }

    /// Returns true if the date is valid (not the Go zero time)
    var hasValidDate: Bool {
        // Go zero time is year 1
        return updatedAt.timeIntervalSince1970 > 0
    }
}

extension Date {
    /// Relative display like "2 min ago", "Yesterday", "Feb 25".
    var relativeDisplay: String {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter.localizedString(for: self, relativeTo: Date())
    }
}
