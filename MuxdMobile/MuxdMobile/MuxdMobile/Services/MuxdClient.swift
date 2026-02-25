import Foundation

actor MuxdClient {
    private let baseURL: URL
    private let authToken: String
    private let session: URLSession

    init(host: String, port: Int, token: String) {
        self.baseURL = URL(string: "http://\(host):\(port)")!
        self.authToken = token

        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 30
        self.session = URLSession(configuration: config)
    }

    private func makeRequest(
        method: String,
        path: String,
        body: Data? = nil,
        queryItems: [URLQueryItem]? = nil
    ) -> URLRequest {
        var components = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        components.queryItems = queryItems

        var request = URLRequest(url: components.url!)
        request.httpMethod = method
        request.setValue("Bearer \(authToken)", forHTTPHeaderField: "Authorization")

        if let body = body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = body
        }

        return request
    }

    // MARK: - Health Check

    func health() async throws -> Bool {
        let request = makeRequest(method: "GET", path: "/api/health")
        let (_, response) = try await session.data(for: request)
        return (response as? HTTPURLResponse)?.statusCode == 200
    }

    // MARK: - Sessions

    func createSession(projectPath: String, modelID: String?) async throws -> String {
        var body: [String: Any] = ["project_path": projectPath]
        if let modelID = modelID {
            body["model_id"] = modelID
        }
        let jsonData = try JSONSerialization.data(withJSONObject: body)

        let request = makeRequest(method: "POST", path: "/api/sessions", body: jsonData)
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to create session")
        }

        let result = try JSONDecoder().decode([String: String].self, from: data)
        guard let sessionID = result["session_id"] else {
            throw MuxdError.invalidResponse
        }
        return sessionID
    }

    func listSessions(project: String?, limit: Int = 10) async throws -> [Session] {
        var queryItems: [URLQueryItem] = [URLQueryItem(name: "limit", value: "\(limit)")]
        if let project = project {
            queryItems.append(URLQueryItem(name: "project", value: project))
        }

        let request = makeRequest(method: "GET", path: "/api/sessions", queryItems: queryItems)
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw MuxdError.invalidResponse
        }

        if httpResponse.statusCode == 401 {
            throw MuxdError.unauthorized
        }

        guard httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to list sessions")
        }

        // Debug: print raw response
        if let jsonString = String(data: data, encoding: .utf8) {
            print("Sessions API response: \(jsonString.prefix(500))...")
        }

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        do {
            return try decoder.decode([Session].self, from: data)
        } catch {
            print("Session decode error: \(error)")
            throw error
        }
    }

    func deleteSession(id: String) async throws {
        let request = makeRequest(method: "DELETE", path: "/api/sessions/\(id)")
        let (_, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw MuxdError.invalidResponse
        }

        if httpResponse.statusCode == 404 {
            throw MuxdError.notFound("Session not found")
        }

        guard httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to delete session")
        }
    }

    func getSession(id: String) async throws -> Session {
        let request = makeRequest(method: "GET", path: "/api/sessions/\(id)")
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw MuxdError.invalidResponse
        }

        if httpResponse.statusCode == 404 {
            throw MuxdError.notFound("Session not found")
        }

        guard httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to get session")
        }

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(Session.self, from: data)
    }

    func getMessages(sessionID: String) async throws -> [TranscriptMessage] {
        let request = makeRequest(method: "GET", path: "/api/sessions/\(sessionID)/messages")
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to get messages")
        }

        // Debug: print raw response
        if let jsonString = String(data: data, encoding: .utf8) {
            print("Messages API response: \(jsonString.prefix(1000))...")
        }

        // Handle empty/null response
        if data.isEmpty {
            return []
        }

        // Check for null response
        if let jsonString = String(data: data, encoding: .utf8), jsonString.trimmingCharacters(in: .whitespaces) == "null" {
            return []
        }

        do {
            return try JSONDecoder().decode([TranscriptMessage].self, from: data)
        } catch {
            print("Message decode error: \(error)")
            // Return empty array instead of throwing for decode errors
            return []
        }
    }

    // MARK: - Session Actions

    func cancel(sessionID: String) async throws {
        let request = makeRequest(method: "POST", path: "/api/sessions/\(sessionID)/cancel")
        let (_, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to cancel session")
        }
    }

    func sendAskResponse(sessionID: String, askID: String, answer: String) async throws {
        let body = try JSONEncoder().encode(["ask_id": askID, "answer": answer])
        let request = makeRequest(method: "POST", path: "/api/sessions/\(sessionID)/ask-response", body: body)
        let (_, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to send ask response")
        }
    }

    func setModel(sessionID: String, label: String, modelID: String) async throws {
        let body = try JSONEncoder().encode(["label": label, "model_id": modelID])
        let request = makeRequest(method: "POST", path: "/api/sessions/\(sessionID)/model", body: body)
        let (_, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to set model")
        }
    }

    func renameSession(sessionID: String, title: String) async throws {
        let body = try JSONEncoder().encode(["title": title])
        let request = makeRequest(method: "POST", path: "/api/sessions/\(sessionID)/title", body: body)
        let (_, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to rename session")
        }
    }

    func branchSession(sessionID: String, atSequence: Int) async throws -> Session {
        let body = try JSONSerialization.data(withJSONObject: ["at_sequence": atSequence])
        let request = makeRequest(method: "POST", path: "/api/sessions/\(sessionID)/branch", body: body)
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to branch session")
        }

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try decoder.decode(Session.self, from: data)
    }

    // MARK: - Config

    func getConfig() async throws -> [String: Any] {
        let request = makeRequest(method: "GET", path: "/api/config")
        let (data, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to get config")
        }

        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }

    func setConfig(key: String, value: String) async throws {
        let body = try JSONEncoder().encode(["key": key, "value": value])
        let request = makeRequest(method: "POST", path: "/api/config", body: body)
        let (_, response) = try await session.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw MuxdError.serverError("Failed to set config")
        }
    }
}

enum MuxdError: Error, LocalizedError {
    case invalidResponse
    case unauthorized
    case connectionFailed
    case serverError(String)
    case notFound(String)

    var errorDescription: String? {
        switch self {
        case .invalidResponse:
            return "Invalid response from server"
        case .unauthorized:
            return "Unauthorized - invalid token"
        case .connectionFailed:
            return "Failed to connect to server"
        case .serverError(let message):
            return message
        case .notFound(let message):
            return message
        }
    }
}
