import SwiftUI

struct AppGlassModifier: ViewModifier {
    var circular: Bool = false

    func body(content: Content) -> some View {
        if circular {
            if #available(iOS 26.0, *) {
                content
                    .frame(width: 44, height: 44)
                    .glassEffect(.regular, in: .circle)
            } else {
                content
                    .frame(width: 44, height: 44)
                    .background(.ultraThinMaterial, in: Circle())
            }
        } else {
            if #available(iOS 26.0, *) {
                content
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .frame(minHeight: 44)
                    .glassEffect(.regular, in: .capsule)
            } else {
                content
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
                    .frame(minHeight: 44)
                    .background(.ultraThinMaterial, in: Capsule())
            }
        }
    }
}

struct AppGlassButtonStyle: ButtonStyle {
    var circular: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .modifier(AppGlassModifier(circular: circular))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

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
                    Image(systemName: "house")
                    Text("Home")
                }

            ClientsView()
                .tabItem {
                    Image(systemName: "server.rack")
                    Text("Clients")
                }

            ConfigView()
                .tabItem {
                    Image(systemName: "gear")
                    Text("Settings")
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
            VStack(spacing: 0) {
                Spacer()

                // Header
                VStack(spacing: 8) {
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
                .padding(.top, 40)

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

struct ClientsView: View {
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
                        Label("No Clients", systemImage: "server.rack")
                    } description: {
                        Text("Add a client to get started")
                    } actions: {
                        Button("Scan QR Code") {
                            showScanner = true
                        }
                        .buttonStyle(.borderedProminent)
                    }
                } else {
                    List {
                        ForEach(Array(appState.savedConnections.enumerated()), id: \.element.id) { index, connection in
                            ClientRowView(
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
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) {
                    HStack(spacing: 8) {
                        Image(systemName: "server.rack")
                        Text("Clients")
                    }
                    .padding(.horizontal, 8)
                    .modifier(AppGlassModifier())
                }
                ToolbarItem(placement: .primaryAction) {
                    Button(action: { showScanner = true }) {
                        Image(systemName: "plus")
                            .font(.system(size: 17, weight: .semibold))
                    }
                    .buttonStyle(AppGlassButtonStyle(circular: true))
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

struct ClientRowView: View {
    let connection: ConnectionInfo
    let isConnecting: Bool
    let isDisabled: Bool
    let onConnect: () -> Void
    let onRename: () -> Void
    let onDelete: () -> Void

    @State private var showSpinner = false
    @State private var spinnerTask: Task<Void, Never>?

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

                ZStack {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundColor(Color(.tertiaryLabel))
                        .opacity(showSpinner ? 0 : 1)

                    if showSpinner {
                        ProgressView()
                            .scaleEffect(0.8)
                    }
                }
                .frame(width: 20, height: 20)
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
        .onChange(of: isConnecting) { _, connecting in
            spinnerTask?.cancel()
            if connecting {
                spinnerTask = Task { @MainActor in
                    try? await Task.sleep(nanoseconds: 300_000_000)
                    guard !Task.isCancelled else { return }
                    withAnimation(.easeIn(duration: 0.15)) {
                        showSpinner = true
                    }
                }
            } else {
                withAnimation {
                    showSpinner = false
                }
            }
        }
        .onDisappear {
            spinnerTask?.cancel()
            showSpinner = false
        }
    }
}
