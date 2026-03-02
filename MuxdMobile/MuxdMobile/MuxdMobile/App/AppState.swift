import Foundation
import Combine
import UIKit

@MainActor
class AppState: ObservableObject {
    @Published var isConnected = false
    @Published var connectionInfo: ConnectionInfo?
    @Published var currentSession: Session?
    @Published var error: AppError?
    @Published var savedConnections: [ConnectionInfo] = []

    // Hub state
    @Published var connectionMode: ConnectionMode = .daemon
    @Published var selectedNode: HubNode?
    @Published var hubNodes: [HubNode] = []

    private var client: MuxdClient?
    private var sseClient: SSEClient?
    private var hubClient: MuxdClient?

    var isHubConnected: Bool {
        connectionMode == .hub && connectionInfo != nil && hubClient != nil
    }

    init() {
        loadSavedConnections()
    }

    // MARK: - Haptics

    private func hapticSuccess() {
        let generator = UINotificationFeedbackGenerator()
        generator.notificationOccurred(.success)
    }

    private func hapticError() {
        let generator = UINotificationFeedbackGenerator()
        generator.notificationOccurred(.error)
    }

    private func hapticLight() {
        let generator = UIImpactFeedbackGenerator(style: .light)
        generator.impactOccurred()
    }

    // MARK: - Connection

    func connect(with info: ConnectionInfo, silent: Bool = false) async {
        guard let newClient = MuxdClient(host: info.host, port: info.port, token: info.token) else {
            if !silent {
                error = .connectionFailed("Invalid server address: \(info.host):\(info.port)")
                hapticError()
            }
            return
        }

        do {
            let healthResp = try await newClient.healthCheck()
            guard healthResp.status == "ok" else {
                if !silent {
                    error = .connectionFailed("Server not responding")
                    hapticError()
                }
                return
            }

            if healthResp.isHub {
                // Hub mode: store hub client, don't set isConnected yet (user must pick node)
                hubClient = newClient
                connectionMode = .hub
                connectionInfo = info
                selectedNode = nil
                isConnected = false
                client = nil
                sseClient = nil
                KeychainHelper.save(connectionInfo: info)
                loadSavedConnections()
                hapticSuccess()

                // Pre-fetch nodes
                await refreshNodes()
            } else {
                // Daemon mode: existing flow
                client = newClient
                sseClient = SSEClient(host: info.host, port: info.port, token: info.token)

                // Verify token is valid by making an authenticated request
                _ = try await newClient.listSessions(project: nil, limit: 1)

                connectionMode = .daemon
                connectionInfo = info
                hubClient = nil
                selectedNode = nil
                hubNodes = []
                isConnected = true
                KeychainHelper.save(connectionInfo: info)
                loadSavedConnections()
                hapticSuccess()
            }
        } catch MuxdError.unauthorized {
            resetConnectionState()
            KeychainHelper.deleteConnectionInfo()
            if !silent {
                error = .connectionFailed("Invalid or expired token")
                hapticError()
            }
        } catch {
            resetConnectionState()
            if !silent {
                self.error = .connectionFailed(error.localizedDescription)
                hapticError()
            }
        }
    }

    private func resetConnectionState() {
        connectionInfo = nil
        isConnected = false
        connectionMode = .daemon
        client = nil
        sseClient = nil
        hubClient = nil
        selectedNode = nil
        hubNodes = []
    }

    func disconnect() {
        sseClient?.cancel()
        resetConnectionState()
        currentSession = nil
        KeychainHelper.deleteConnectionInfo()
        hapticLight()
    }

    func restoreConnection() async {
        if let savedInfo = KeychainHelper.loadConnectionInfo() {
            await connect(with: savedInfo, silent: true)
            // For hub mode, don't auto-connect to a node — user must re-pick
        }
    }

    // MARK: - Hub

    func refreshNodes() async {
        guard let hubClient = hubClient else { return }
        do {
            let nodes = try await hubClient.listNodes()
            // Sort: online first, then by name
            hubNodes = nodes.sorted { n1, n2 in
                if n1.isOnline != n2.isOnline {
                    return n1.isOnline
                }
                return n1.name.localizedCaseInsensitiveCompare(n2.name) == .orderedAscending
            }
        } catch {
            self.error = .serverError("Failed to load nodes: \(error.localizedDescription)")
        }
    }

    func selectNode(_ node: HubNode) {
        guard let info = connectionInfo else { return }

        guard let proxyClient = MuxdClient(hubHost: info.host, hubPort: info.port, nodeID: node.id, token: info.token) else {
            error = .connectionFailed("Failed to create proxy connection for node \(node.name)")
            return
        }

        guard let proxySse = SSEClient(hubHost: info.host, hubPort: info.port, nodeID: node.id, token: info.token) else {
            error = .connectionFailed("Failed to create SSE connection for node \(node.name)")
            return
        }

        client = proxyClient
        sseClient = proxySse
        selectedNode = node
        isConnected = true
        #if DEBUG
        print("[AppState] selectNode: \(node.name) (id: \(node.id)), proxy base: \(info.host):\(info.port)/api/hub/proxy/\(node.id)")
        #endif
        hapticSuccess()
    }

    func deselectNode() {
        sseClient?.cancel()
        client = nil
        sseClient = nil
        selectedNode = nil
        currentSession = nil
        isConnected = false
    }

    // MARK: - Saved Connections

    func loadSavedConnections() {
        savedConnections = KeychainHelper.loadConnections()
    }

    func removeConnection(id: String) {
        KeychainHelper.removeConnection(id: id)
        loadSavedConnections()
    }

    func renameConnection(id: String, name: String) {
        KeychainHelper.updateConnectionName(id: id, name: name)
        loadSavedConnections()
        if connectionInfo?.id == id {
            connectionInfo?.name = name
        }
    }

    // MARK: - Accessors

    func getClient() -> MuxdClient? {
        return client
    }

    func getSSEClient() -> SSEClient? {
        return sseClient
    }
}

enum AppError: LocalizedError, Identifiable {
    case connectionFailed(String)
    case serverError(String)
    case invalidQRCode

    var id: String {
        switch self {
        case .connectionFailed(let msg): return "connection_\(msg)"
        case .serverError(let msg): return "server_\(msg)"
        case .invalidQRCode: return "invalid_qr"
        }
    }

    var errorDescription: String? {
        switch self {
        case .connectionFailed(let msg): return "Connection failed: \(msg)"
        case .serverError(let msg): return "Server error: \(msg)"
        case .invalidQRCode: return "Invalid QR code format"
        }
    }
}
