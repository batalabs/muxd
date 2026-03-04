import SwiftUI

struct ManualConnectionView: View {
    @Environment(\.dismiss) private var dismiss
    @EnvironmentObject var appState: AppState
    @State private var name = ""
    @State private var host = ""
    @State private var port = "4096"
    @State private var token = ""
    @State private var isConnecting = false
    @State private var error: String?

    var prefill: ConnectionInfo?
    let onConnected: ((ConnectionInfo) -> Void)?

    init(prefill: ConnectionInfo? = nil, onConnected: ((ConnectionInfo) -> Void)? = nil) {
        self.prefill = prefill
        self.onConnected = onConnected
    }

    var isValid: Bool {
        !host.isEmpty && !port.isEmpty && !token.isEmpty
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Name (optional)", text: $name)
                        .autocapitalization(.words)
                } footer: {
                    Text("A friendly name for this server")
                }

                Section {
                    TextField("Host (IP or hostname)", text: $host)
                        .textContentType(.URL)
                        .autocapitalization(.none)
                        .keyboardType(.URL)

                    TextField("Port", text: $port)
                        .keyboardType(.numberPad)

                    SecureField("Token", text: $token)
                        .textContentType(.password)
                        .autocapitalization(.none)
                } header: {
                    Text("Server Connection")
                } footer: {
                    Text("Find these values in the muxd lockfile at ~/.local/share/muxd/server.lock")
                }

                if let error = error {
                    Section {
                        Label {
                            Text(error)
                        } icon: {
                            Image(systemName: "exclamationmark.triangle.fill")
                                .foregroundColor(.red)
                        }
                        .foregroundColor(.red)
                    }
                }

                Section {
                    Label {
                        Text("Connections are unencrypted. Only connect over a trusted network such as a VPN or local network.")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    } icon: {
                        Image(systemName: "lock.shield")
                            .foregroundColor(.orange)
                    }
                }
            }
            .navigationTitle(prefill != nil ? "Confirm Connection" : "Manual Connection")
            .navigationBarTitleDisplayMode(.inline)
            .onAppear {
                if let info = prefill {
                    host = info.host
                    port = String(info.port)
                    token = info.token
                    name = info.name != info.host ? info.name : ""
                }
            }
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                        .disabled(isConnecting)
                }
                ToolbarItem(placement: .confirmationAction) {
                    if isConnecting {
                        ProgressView()
                    } else {
                        Button("Connect") {
                            connect()
                        }
                        .disabled(!isValid)
                    }
                }
            }
            .disabled(isConnecting)
            .interactiveDismissDisabled(isConnecting)
        }
    }

    private func connect() {
        guard let portNum = Int(port) else {
            error = "Invalid port number"
            return
        }

        isConnecting = true
        error = nil

        let trimmedHost = host.trimmingCharacters(in: .whitespaces)
        let serverName = name.isEmpty ? trimmedHost : name.trimmingCharacters(in: .whitespaces)

        let info = ConnectionInfo(
            host: trimmedHost,
            port: portNum,
            token: token.trimmingCharacters(in: .whitespaces),
            name: serverName
        )

        Task {
            await appState.connect(with: info)

            if appState.error != nil {
                error = appState.error?.localizedDescription ?? "Connection failed"
                appState.error = nil
                isConnecting = false
            } else {
                isConnecting = false
                onConnected?(info)
                dismiss()
            }
        }
    }
}
