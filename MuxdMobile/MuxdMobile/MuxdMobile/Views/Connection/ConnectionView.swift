import SwiftUI

struct ConnectionView: View {
    @EnvironmentObject var appState: AppState
    @Binding var selectedTab: Int
    @State private var showScanner = false
    @State private var showManual = false
    @State private var connectionToRename: ConnectionInfo?
    @State private var connectingToID: String? = nil

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                // Logo header
                VStack(spacing: 8) {
                    Image("Logo")
                        .resizable()
                        .aspectRatio(contentMode: .fit)
                        .frame(width: 80, height: 80)
                        .cornerRadius(16)

                    Text("muxd")
                        .font(.system(size: 36, weight: .bold))

                    Text("AI Coding Agent")
                        .font(.subheadline)
                        .foregroundColor(.secondary)
                }
                .padding(.top, 20)
                .padding(.bottom, 24)

                // Add new connection buttons
                VStack(spacing: 12) {
                    Button(action: { showScanner = true }) {
                        Label("Scan QR Code", systemImage: "qrcode.viewfinder")
                            .frame(maxWidth: .infinity)
                            .padding()
                            .background(Color.accentColor)
                            .foregroundColor(.white)
                            .cornerRadius(12)
                    }
                    .disabled(connectingToID != nil)

                    Button(action: { showManual = true }) {
                        Label("Enter Manually", systemImage: "keyboard")
                            .frame(maxWidth: .infinity)
                            .padding()
                            .background(Color(.systemGray5))
                            .foregroundColor(.primary)
                            .cornerRadius(12)
                    }
                    .disabled(connectingToID != nil)
                }
                .padding(.horizontal, 24)
                .padding(.bottom, 24)

                // Saved connections (scrollable)
                if !appState.savedConnections.isEmpty {
                    VStack(alignment: .leading, spacing: 0) {
                        Text("Saved Servers")
                            .font(.headline)
                            .padding(.horizontal, 24)
                            .padding(.bottom, 12)

                        List {
                            ForEach(appState.savedConnections) { connection in
                                Button {
                                    connectTo(connection)
                                } label: {
                                    SavedConnectionRowCompact(
                                        connection: connection,
                                        isConnecting: connectingToID == connection.id,
                                        isDisabled: connectingToID != nil && connectingToID != connection.id
                                    )
                                }
                                .buttonStyle(.plain)
                                .id(connection.id)
                                .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                                    Button(role: .destructive) {
                                        withAnimation {
                                            appState.removeConnection(id: connection.id)
                                        }
                                    } label: {
                                        Label("Delete", systemImage: "trash")
                                    }
                                }
                                .swipeActions(edge: .leading) {
                                    Button {
                                        connectionToRename = connection
                                    } label: {
                                        Label("Rename", systemImage: "pencil")
                                    }
                                    .tint(.blue)
                                }
                                .contextMenu {
                                    Button {
                                        connectionToRename = connection
                                    } label: {
                                        Label("Rename", systemImage: "pencil")
                                    }
                                    Button(role: .destructive) {
                                        withAnimation {
                                            appState.removeConnection(id: connection.id)
                                        }
                                    } label: {
                                        Label("Delete", systemImage: "trash")
                                    }
                                }
                            }
                            .onDelete { indexSet in
                                for index in indexSet {
                                    let connection = appState.savedConnections[index]
                                    appState.removeConnection(id: connection.id)
                                }
                            }
                        }
                        .listStyle(.plain)
                        .scrollIndicators(.hidden)
                    }
                } else {
                    Spacer()

                    Text("Run /qr in muxd to display the connection QR code")
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal)

                    Spacer()
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button(action: { showScanner = true }) {
                        Image(systemName: "plus")
                    }
                }
            }
        }
        .sheet(isPresented: $showScanner) {
            QRScannerView { info in
                Task {
                    await appState.connect(with: info)
                    if appState.isConnected {
                        selectedTab = 1 // Switch to Sessions tab
                    }
                }
            }
        }
        .sheet(isPresented: $showManual) {
            ManualConnectionView { info in
                Task {
                    await appState.connect(with: info)
                    if appState.isConnected {
                        selectedTab = 1 // Switch to Sessions tab
                    }
                }
            }
        }
        .sheet(item: $connectionToRename) { connection in
            RenameConnectionView(connection: connection) { newName in
                appState.renameConnection(id: connection.id, name: newName)
            }
        }
        .alert(item: $appState.error) { error in
            Alert(
                title: Text("Connection Error"),
                message: Text(error.localizedDescription),
                dismissButton: .default(Text("OK"))
            )
        }
        .task {
            await appState.restoreConnection()
        }
    }

    private func connectTo(_ connection: ConnectionInfo) {
        guard connectingToID == nil else { return }
        connectingToID = connection.id
        Task { @MainActor in
            await appState.connect(with: connection)
            connectingToID = nil
            if appState.isConnected {
                selectedTab = 1 // Switch to Sessions tab
            }
        }
    }
}

struct SavedConnectionRowCompact: View {
    let connection: ConnectionInfo
    let isConnecting: Bool
    let isDisabled: Bool

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "server.rack")
                .font(.title2)
                .foregroundColor(.accentColor)
                .frame(width: 32)

            VStack(alignment: .leading, spacing: 2) {
                Text(connection.name)
                    .font(.headline)
                    .foregroundColor(.primary)

                Text("\(connection.host):\(String(connection.port))")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            Spacer()

            if isConnecting {
                ProgressView()
                    .scaleEffect(0.8)
            } else {
                Image(systemName: "chevron.right")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .contentShape(Rectangle())
        .opacity(isDisabled ? 0.5 : 1.0)
    }
}

struct SavedConnectionRow: View {
    let connection: ConnectionInfo
    let isConnecting: Bool
    let isDisabled: Bool
    let onConnect: () -> Void
    let onRename: () -> Void
    let onDelete: () -> Void

    var body: some View {
        HStack(spacing: 8) {
            Button(action: {
                if !isConnecting && !isDisabled {
                    onConnect()
                }
            }) {
                HStack(spacing: 12) {
                    Image(systemName: "server.rack")
                        .font(.title2)
                        .foregroundColor(.accentColor)
                        .frame(width: 40)

                    VStack(alignment: .leading, spacing: 2) {
                        Text(connection.name)
                            .font(.headline)
                            .foregroundColor(.primary)

                        Text("\(connection.host):\(String(connection.port))")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }

                    Spacer()

                    if isConnecting {
                        ProgressView()
                            .scaleEffect(0.8)
                    } else {
                        Image(systemName: "chevron.right")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                }
                .padding()
                .background(Color(.systemGray6))
                .cornerRadius(12)
                .contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            .opacity(isDisabled ? 0.5 : 1.0)
            .contextMenu {
                Button(action: onRename) {
                    Label("Rename", systemImage: "pencil")
                }
                Button(role: .destructive, action: onDelete) {
                    Label("Delete", systemImage: "trash")
                }
            }

            Button(action: onDelete) {
                Image(systemName: "xmark.circle.fill")
                    .font(.title2)
                    .foregroundColor(.secondary)
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 24)
    }
}

struct RenameConnectionView: View {
    @Environment(\.dismiss) private var dismiss
    let connection: ConnectionInfo
    let onRename: (String) -> Void

    @State private var name: String = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Server Name", text: $name)
                        .autocapitalization(.words)
                } header: {
                    Text("Name")
                } footer: {
                    Text("Enter a friendly name for this server")
                }

                Section {
                    LabeledContent("Host", value: connection.host)
                    LabeledContent("Port", value: "\(connection.port)")
                }
            }
            .navigationTitle("Rename Server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        onRename(name)
                        dismiss()
                    }
                    .disabled(name.isEmpty)
                }
            }
            .onAppear {
                name = connection.name
            }
        }
    }
}

