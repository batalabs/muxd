import SwiftUI

@main
struct MuxdMobileApp: App {
    @StateObject private var appState = AppState()
    @AppStorage("appTheme") private var appTheme: AppTheme = .system

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(appState)
                .preferredColorScheme(appTheme.colorScheme)
        }
    }
}

struct ContentView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        TabView {
            HomeView()
                .tabItem {
                    Label("Home", systemImage: "house")
                }

            ServersView()
                .tabItem {
                    Label("Servers", systemImage: "server.rack")
                }

            ConfigView()
                .tabItem {
                    Label("Settings", systemImage: "gear")
                }
        }
    }
}

struct HomeView: View {
    @EnvironmentObject var appState: AppState
    @State private var showScanner = false
    @State private var showManual = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 24) {
                Spacer()

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

                // Quick connect buttons
                VStack(spacing: 12) {
                    Button(action: { showScanner = true }) {
                        Label("Scan QR Code", systemImage: "qrcode.viewfinder")
                            .frame(maxWidth: .infinity)
                            .padding()
                            .background(Color.accentColor)
                            .foregroundColor(.white)
                            .cornerRadius(12)
                    }

                    Button(action: { showManual = true }) {
                        Label("Enter Manually", systemImage: "keyboard")
                            .frame(maxWidth: .infinity)
                            .padding()
                            .background(Color(.systemGray5))
                            .foregroundColor(.primary)
                            .cornerRadius(12)
                    }

                    Text("Run /qr in muxd to display the connection QR code")
                        .font(.caption)
                        .foregroundColor(.secondary)
                        .multilineTextAlignment(.center)
                        .padding(.top, 8)
                }
                .padding(.horizontal, 24)

                Spacer()
                Spacer()
            }
            .navigationBarTitleDisplayMode(.inline)
        }
        .sheet(isPresented: $showScanner) {
            QRScannerView { info in
                Task { await appState.connect(with: info) }
            }
        }
        .sheet(isPresented: $showManual) {
            ManualConnectionView { info in
                Task { await appState.connect(with: info) }
            }
        }
        .alert(item: $appState.error) { error in
            Alert(
                title: Text("Connection Error"),
                message: Text(error.localizedDescription),
                dismissButton: .default(Text("OK"))
            )
        }
    }
}

struct ServersView: View {
    @EnvironmentObject var appState: AppState
    @State private var navigationPath = NavigationPath()
    @State private var showScanner = false
    @State private var showManual = false
    @State private var connectionToRename: ConnectionInfo?
    @State private var connectingToID: String? = nil

    var body: some View {
        NavigationStack(path: $navigationPath) {
            Group {
                if appState.savedConnections.isEmpty {
                    ContentUnavailableView {
                        Label("No Servers", systemImage: "server.rack")
                    } description: {
                        Text("Add a server to get started")
                    } actions: {
                        Button("Scan QR Code") {
                            showScanner = true
                        }
                        .buttonStyle(.borderedProminent)
                    }
                } else {
                    List {
                        ForEach(Array(appState.savedConnections.enumerated()), id: \.element.id) { index, connection in
                            ServerRowView(
                                connection: connection,
                                isConnecting: connectingToID == connection.id,
                                isDisabled: connectingToID != nil && connectingToID != connection.id,
                                onConnect: { connectTo(connection) },
                                onRename: { connectionToRename = connection },
                                onDelete: {
                                    withAnimation {
                                        appState.removeConnection(id: connection.id)
                                    }
                                }
                            )
                            .listRowSeparator(index == 0 ? .hidden : .visible, edges: .top)
                        }
                        .onDelete { indexSet in
                            for index in indexSet {
                                let connection = appState.savedConnections[index]
                                appState.removeConnection(id: connection.id)
                            }
                        }
                    }
                    .listStyle(.plain)
                }
            }
            .navigationTitle("Servers")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button(action: { showScanner = true }) {
                        Image(systemName: "plus")
                    }
                }
            }
            .navigationDestination(for: String.self) { _ in
                SessionListView()
            }
        }
        .sheet(isPresented: $showScanner) {
            QRScannerView { info in
                Task {
                    await appState.connect(with: info)
                    if appState.isConnected {
                        navigationPath.append("sessions")
                    }
                }
            }
        }
        .sheet(isPresented: $showManual) {
            ManualConnectionView { info in
                Task {
                    await appState.connect(with: info)
                    if appState.isConnected {
                        navigationPath.append("sessions")
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
    }

    private func connectTo(_ connection: ConnectionInfo) {
        guard connectingToID == nil else { return }
        connectingToID = connection.id
        Task { @MainActor in
            await appState.connect(with: connection)
            connectingToID = nil
            if appState.isConnected {
                navigationPath.append("sessions")
            }
        }
    }
}

struct ServerRowView: View {
    let connection: ConnectionInfo
    let isConnecting: Bool
    let isDisabled: Bool
    let onConnect: () -> Void
    let onRename: () -> Void
    let onDelete: () -> Void

    var body: some View {
        Button(action: {
            if !isConnecting && !isDisabled {
                onConnect()
            }
        }) {
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
        }
        .buttonStyle(.plain)
        .opacity(isDisabled ? 0.5 : 1.0)
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button(role: .destructive, action: onDelete) {
                Label("Delete", systemImage: "trash")
            }
        }
        .swipeActions(edge: .leading) {
            Button(action: onRename) {
                Label("Rename", systemImage: "pencil")
            }
            .tint(.blue)
        }
        .contextMenu {
            Button(action: onRename) {
                Label("Rename", systemImage: "pencil")
            }
            Button(role: .destructive, action: onDelete) {
                Label("Delete", systemImage: "trash")
            }
        }
    }
}
