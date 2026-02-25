import SwiftUI

struct ManualConnectionView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var host = ""
    @State private var port = "4096"
    @State private var token = ""
    @State private var isConnecting = false
    @State private var error: String?

    let onConnect: (ConnectionInfo) -> Void

    var isValid: Bool {
        !host.isEmpty && !port.isEmpty && !token.isEmpty
    }

    var body: some View {
        NavigationStack {
            Form {
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
                        Text(error)
                            .foregroundColor(.red)
                    }
                }
            }
            .navigationTitle("Manual Connection")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Connect") {
                        connect()
                    }
                    .disabled(!isValid || isConnecting)
                }
            }
            .overlay {
                if isConnecting {
                    ProgressView("Connecting...")
                        .padding()
                        .background(.regularMaterial)
                        .cornerRadius(8)
                }
            }
        }
    }

    private func connect() {
        guard let portNum = Int(port) else {
            error = "Invalid port number"
            return
        }

        isConnecting = true
        error = nil

        let info = ConnectionInfo(
            host: host.trimmingCharacters(in: .whitespaces),
            port: portNum,
            token: token.trimmingCharacters(in: .whitespaces)
        )

        onConnect(info)
        dismiss()
    }
}
