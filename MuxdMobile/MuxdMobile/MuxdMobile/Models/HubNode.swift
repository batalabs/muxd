import Foundation

struct HubNode: Codable, Identifiable, Hashable, Sendable {
    let id: String
    let name: String
    let host: String
    let port: Int
    let version: String
    let status: NodeStatus
    let registeredAt: Date
    let lastSeenAt: Date

    var isOnline: Bool { status == .online }

    enum NodeStatus: String, Codable {
        case online
        case offline
    }

    enum CodingKeys: String, CodingKey {
        case id, name, host, port, version, status
        case registeredAt = "registered_at"
        case lastSeenAt = "last_seen_at"
    }
}

enum ConnectionMode: String, Codable {
    case daemon
    case hub
}

struct HealthResponse: Codable, Sendable {
    let status: String
    let mode: String?
    let port: Int?
    let pid: Int?

    var isHub: Bool { mode == "hub" }
}
