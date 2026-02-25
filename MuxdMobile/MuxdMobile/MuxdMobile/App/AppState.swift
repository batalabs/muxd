import Foundation
import Combine

@MainActor
class AppState: ObservableObject {
    @Published var isConnected = false
    @Published var connectionInfo: ConnectionInfo?
    @Published var currentSession: Session?
    @Published var error: AppError?

    private var client: MuxdClient?
    private var sseClient: SSEClient?

    // MARK: - Connection

    func connect(with info: ConnectionInfo) async {
        print("AppState: connecting to \(info.host):\(info.port)")
        client = MuxdClient(host: info.host, port: info.port, token: info.token)
        sseClient = SSEClient(host: info.host, port: info.port, token: info.token)

        do {
            print("AppState: checking health...")
            let healthy = try await client?.health() ?? false
            print("AppState: health check result: \(healthy)")
            if healthy {
                // Verify token is valid by calling an authenticated endpoint
                print("AppState: verifying token...")
                _ = try await client?.listSessions(project: nil, limit: 1)

                connectionInfo = info
                isConnected = true
                // Save connection info to keychain
                print("AppState: saving to keychain...")
                KeychainHelper.save(connectionInfo: info)
                print("AppState: connected successfully!")
            } else {
                print("AppState: server not responding")
                connectionInfo = nil
                isConnected = false
                error = .connectionFailed("Server not responding")
            }
        } catch MuxdError.unauthorized {
            print("AppState: invalid token")
            connectionInfo = nil
            isConnected = false
            client = nil
            sseClient = nil
            KeychainHelper.deleteConnectionInfo()
            // Don't show error - just go to QR scanner
        } catch {
            print("AppState: connection error: \(error)")
            connectionInfo = nil
            isConnected = false
            self.error = .connectionFailed(error.localizedDescription)
        }
    }

    func disconnect() {
        isConnected = false
        connectionInfo = nil
        client = nil
        sseClient = nil
        currentSession = nil
        KeychainHelper.deleteConnectionInfo()
    }

    func restoreConnection() async {
        print("AppState: trying to restore connection from keychain...")
        if let savedInfo = KeychainHelper.loadConnectionInfo() {
            print("AppState: found saved connection to \(savedInfo.host):\(savedInfo.port)")
            await connect(with: savedInfo)
        } else {
            print("AppState: no saved connection found")
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
