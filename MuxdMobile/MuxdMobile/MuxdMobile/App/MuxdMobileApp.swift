import SwiftUI

struct AppGlassModifier: ViewModifier {
    var circular: Bool = false

    func body(content: View) -> some View {
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
                    ScrollView {
                        LazyVGrid(columns: [
                            GridItem(.flexible(), spacing: 12),
                            GridItem(.flexible(), spacing: 12)
                        ], spacing: 12) {
                            ForEach(appState.savedConnections) { connection in
                                ClientCardView(
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
                                .id(connection.id)
                            }
                        }
                        .padding(.horizontal, 16)
                        .padding(.top, 8)
                    }
                    .scrollIndicators(.hidden)
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

struct ClientCardView: View {
    let connection: ConnectionInfo
    let isConnecting: Bool
    let isDisabled: Bool
    let onConnect: () -> Void
    let onRename: () -> Void
    let onDelete: () -> Void

    @State private var showSpinner = false
    @State private var spinnerTask: Task<Void, Never>?
    @State private var sessionCount: Int?
    @State private var latencyMs: Int?
    @GestureState private var isPressed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: "server.rack")
                    .font(.title2)
                    .foregroundColor(.accentColor)

                Spacer()

                if showSpinner {
                    ProgressView()
                        .scaleEffect(0.8)
                }
            }

            VStack(alignment: .leading, spacing: 2) {
                Text(connection.name)
                    .font(.headline)
                    .foregroundColor(.primary)
                    .lineLimit(1)

                Text("\(connection.host):\(String(connection.port))")
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .lineLimit(1)
            }

            Spacer(minLength: 4)

            HStack {
                if let count = sessionCount {
                    Text("\(count) sessions")
                        .font(.caption)
                        .foregroundColor(.secondary)
                } else {
                    Text("—")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }

                Spacer()

                if let ms = latencyMs {
                    HStack(spacing: 3) {
                        Image(systemName: "bolt.fill")
                            .font(.system(size: 10))
                        Text("\(ms)ms")
                    }
                    .font(.caption)
                    .foregroundColor(.green)
                }
            }
        }
        .padding()
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(isPressed ? Color(.systemGray5) : Color(.systemGray6))
        .cornerRadius(12)
        .scaleEffect(isPressed ? 0.98 : 1.0)
        .animation(.easeInOut(duration: 0.1), value: isPressed)
        .contentShape(Rectangle())
        .gesture(
            LongPressGesture(minimumDuration: 0.01)
                .updating($isPressed) { currentState, gestureState, transaction in
                    transaction.animation = nil
                    gestureState = currentState
                }
                .onEnded { _ in
                    if !isDisabled {
                        onConnect()
                    }
                }
        )
        .opacity(isDisabled ? 0.5 : 1.0)
        .disabled(isDisabled)
        .overlay(alignment: .topTrailing) {
            if !showSpinner {
                Menu {
                    Button(action: onRename) {
                        Label("Rename", systemImage: "pencil")
                    }
                    Button(role: .destructive, action: onDelete) {
                        Label("Delete", systemImage: "trash")
                    }
                } label: {
                    Image(systemName: "ellipsis")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundColor(.secondary)
                        .frame(width: 28, height: 28)
                        .background(Color(.systemGray5))
                        .clipShape(Circle())
                }
                .padding(12)
            }
        }
        .task {
            await loadServerInfo()
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

    private func loadServerInfo() async {
        guard let client = MuxdClient(host: connection.host, port: connection.port, token: connection.token) else {
            return
        }

        // Measure latency with health check
        let start = Date()
        do {
            _ = try await client.health()
            let elapsed = Date().timeIntervalSince(start)
            let ms = Int(elapsed * 1000)
            await MainActor.run {
                latencyMs = ms
            }
        } catch {
            // Ignore - won't show latency
        }

        // Get session count
        do {
            let sessions = try await client.listSessions(project: nil, limit: 100)
            await MainActor.run {
                sessionCount = sessions.count
            }
        } catch {
            // Ignore - won't show session count
        }
    }
}

struct AppTheme: RawRepresentable, Equatable, CaseIterable {
    var rawValue: String
    static let system = AppTheme(rawValue: "system")
    static let light = AppTheme(rawValue: "light")
    static let dark = AppTheme(rawValue: "dark")

    static var allCases: [AppTheme] = [.system, .light, .dark]

    var colorScheme: ColorScheme? {
        switch self {
        case .light: return .light
        case .dark: return .dark
        default: return nil
        }
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

struct ConfigView: View {
    @EnvironmentObject var appState: AppState
    @AppStorage("appTheme") private var appTheme: AppTheme = .system
    @State private var showDisconnectConfirm = false
    @State private var showDeleteConfirm = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Appearance", selection: $appTheme) {
                        Label("System", systemImage: "circle.lefthalf.filled").tag(AppTheme.system)
                        Label("Light", systemImage: "sun.max.fill").tag(AppTheme.light)
                        Label("Dark", systemImage: "moon.fill").tag(AppTheme.dark)
                    }
                }

                if appState.isConnected, let info = appState.connectionInfo {
                    Section("Current Connection") {
                        LabeledContent("Name", value: info.name)
                        LabeledContent("Host", value: info.host)
                        LabeledContent("Port", value: "\(info.port)")

                        Button(role: .destructive) {
                            showDisconnectConfirm = true
                        } label: {
                            Label("Disconnect", systemImage: "xmark.circle.fill")
                        }
                    }
                }

                Section {
                    Button(role: .destructive) {
                        showDeleteConfirm = true
                    } label: {
                        Label("Delete All Saved Clients", systemImage: "trash.fill")
                    }
                    .disabled(appState.savedConnections.isEmpty)
                }
            }
            .navigationTitle("Settings")
            .confirmationDialog("Disconnect from server?", isPresented: $showDisconnectConfirm, titleVisibility: .visible) {
                Button("Disconnect", role: .destructive) {
                    appState.disconnect()
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("You will need to reconnect to access your sessions.")
            }
            .confirmationDialog("Delete all saved clients?", isPresented: $showDeleteConfirm, titleVisibility: .visible) {
                Button("Delete All", role: .destructive) {
                    for connection in appState.savedConnections {
                        appState.removeConnection(id: connection.id)
                    }
                    appState.disconnect()
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("This will remove all saved client connections from this device.")
            }
        }
    }
}
