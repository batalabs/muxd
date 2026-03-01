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

    private var client: MuxdClient?
    private var sseClient: SSEClient?

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

        client = newClient
        sseClient = SSEClient(host: info.host, port: info.port, token: info.token)

        do {
            let healthy = try await newClient.health()
            if healthy {
                // Verify token is valid by making an authenticated request
                _ = try await newClient.listSessions(project: nil, limit: 1)

                connectionInfo = info
                isConnected = true
                KeychainHelper.save(connectionInfo: info)
                loadSavedConnections()
                hapticSuccess()
            } else {
                connectionInfo = nil
                isConnected = false
                client = nil
                sseClient = nil
                if !silent {
                    error = .connectionFailed("Server not responding")
                    hapticError()
                }
            }
        } catch MuxdError.unauthorized {
            connectionInfo = nil
            isConnected = false
            client = nil
            sseClient = nil
            KeychainHelper.deleteConnectionInfo()
            if !silent {
                error = .connectionFailed("Invalid or expired token")
                hapticError()
            }
        } catch {
            connectionInfo = nil
            isConnected = false
            client = nil
            sseClient = nil
            if !silent {
                self.error = .connectionFailed(error.localizedDescription)
                hapticError()
            }
        }
    }

    func disconnect() {
        isConnected = false
        connectionInfo = nil
        client = nil
        sseClient = nil
        currentSession = nil
        KeychainHelper.deleteConnectionInfo()
        hapticLight()
    }

    func restoreConnection() async {
        if let savedInfo = KeychainHelper.loadConnectionInfo() {
            await connect(with: savedInfo, silent: true)
        }
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
