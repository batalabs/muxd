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
    @StateObject private var biometricManager = BiometricManager()
    @AppStorage("appTheme") private var appTheme: AppTheme = .system
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(appState)
                .environmentObject(biometricManager)
                .preferredColorScheme(appTheme.colorScheme)
                .overlay {
                    if biometricManager.isLocked {
                        LockScreenView()
                            .environmentObject(biometricManager)
                    }
                }
        }
        .onChange(of: scenePhase) { _, newPhase in
            if newPhase == .background {
                biometricManager.lockIfEnabled()
            }
        }
    }
}

struct LockScreenView: View {
    @EnvironmentObject var biometricManager: BiometricManager
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        ZStack {
            (colorScheme == .dark ? Color.black : Color.white)
                .ignoresSafeArea()

            VStack(spacing: 24) {
                Image("Logo")
                    .resizable()
                    .frame(width: 80, height: 80)
                    .cornerRadius(16)

                Text("muxd")
                    .font(.system(size: 32, weight: .bold))

                Text("Locked")
                    .font(.subheadline)
                    .foregroundColor(.secondary)

                Button {
                    Task {
                        await biometricManager.authenticate()
                    }
                } label: {
                    Label("Unlock with \(biometricManager.biometricTypeName)", systemImage: biometricManager.biometricIcon)
                        .font(.headline)
                        .padding()
                        .frame(maxWidth: .infinity)
                        .background(Color.accentColor)
                        .foregroundColor(.white)
                        .cornerRadius(12)
                }
                .padding(.horizontal, 40)
                .padding(.top, 20)
            }
        }
        .task {
            _ = await biometricManager.authenticate()
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
    @State private var tokenToShow: ConnectionInfo?

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
                                    },
                                    onViewToken: { tokenToShow = connection },
                                    onDisconnect: {
                                        appState.removeConnection(id: connection.id)
                                    }
                                )
                                .id(connection.id)
                            }
                        }
                        .padding(.horizontal, 16)
                        .padding(.top, 8)
                    }
                    .refreshable {
                        // Trigger a re-render by briefly toggling state
                        try? await Task.sleep(nanoseconds: 500_000_000)
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
            .navigationDestination(for: String.self) { destination in
                switch destination {
                case "nodePicker":
                    NodePickerView(navigationPath: $navigationPath)
                default:
                    SessionListView()
                }
            }
            .navigationDestination(for: Session.self) { session in
                ChatView(session: session)
            }
        }
        .onChange(of: appState.isConnected) { _, connected in
            if !connected && appState.isHubConnected {
                // Node deselected — pop back to node picker
                // Path is ["nodePicker", "sessions"] or ["nodePicker", "sessions", Session...]
                // Reset to just ["nodePicker"]
                var newPath = NavigationPath()
                newPath.append("nodePicker")
                navigationPath = newPath
            } else if !connected {
                // Fully disconnected — pop to root
                navigationPath = NavigationPath()
            }
        }
        .sheet(isPresented: $showScanner) {
            QRScannerView { info in
                Task {
                    await appState.connect(with: info)
                    if appState.connectionMode == .hub {
                        navigationPath.append("nodePicker")
                    } else if appState.isConnected {
                        navigationPath.append("sessions")
                    }
                }
            }
        }
        .sheet(isPresented: $showManual) {
            ManualConnectionView { info in
                Task {
                    await appState.connect(with: info)
                    if appState.connectionMode == .hub {
                        navigationPath.append("nodePicker")
                    } else if appState.isConnected {
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
        .sheet(item: $tokenToShow) { connection in
            TokenView(token: connection.token)
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
            async let connect: () = appState.connect(with: connection)
            async let delay: () = Task.sleep(nanoseconds: 2_000_000_000)
            _ = await (connect, try? delay)
            connectingToID = nil
            if appState.connectionMode == .hub {
                navigationPath.append("nodePicker")
            } else if appState.isConnected {
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
    let onViewToken: () -> Void
    let onDisconnect: () -> Void

    @State private var showSpinner = false
    @State private var spinnerTask: Task<Void, Never>?
    @State private var sessionCount: Int?
    @State private var latencyMs: Int?
    @State private var isHealthy: Bool?
    @State private var detectedMode: ConnectionMode?
    @GestureState private var isPressed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: detectedMode == .hub ? "point.3.connected.trianglepath.dotted" : "server.rack")
                    .font(.title2)
                    .foregroundColor(.accentColor)

                if detectedMode == .hub {
                    Text("Hub")
                        .font(.caption2)
                        .fontWeight(.semibold)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 2)
                        .background(Color.orange)
                        .foregroundColor(.white)
                        .clipShape(Capsule())
                }

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
                    Text(detectedMode == .hub ? "\(count) nodes online" : "\(count) sessions")
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
                    Section {
                        Label(connection.host, systemImage: "server.rack")
                        Label("\(String(connection.port))", systemImage: "number")
                    }

                    Section {
                        if let healthy = isHealthy {
                            Label(healthy ? "Connected" : "Unreachable", systemImage: healthy ? "circle.fill" : "circle")
                        }
                        if let ms = latencyMs {
                            Label("\(ms)ms", systemImage: "clock")
                        }
                    }

                    Section {
                        Button(action: onViewToken) {
                            Label("View Token", systemImage: "key")
                        }
                    }

                    Section {
                        Button(action: onRename) {
                            Label("Rename", systemImage: "character.cursor.ibeam")
                        }
                        Button(role: .destructive, action: onDelete) {
                            Label("Disconnect", systemImage: "xmark.circle")
                        }
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

        // Measure latency with health check and detect mode
        let start = Date()
        do {
            let healthResp = try await client.healthCheck()
            let elapsed = Date().timeIntervalSince(start)
            let ms = Int(elapsed * 1000)
            let mode: ConnectionMode = healthResp.isHub ? .hub : .daemon
            await MainActor.run {
                isHealthy = healthResp.status == "ok"
                latencyMs = ms
                detectedMode = mode
            }
        } catch {
            await MainActor.run {
                isHealthy = false
            }
        }

        // Get session count (only for daemon mode)
        if detectedMode != .hub {
            do {
                let sessions = try await client.listSessions(project: nil, limit: 100)
                await MainActor.run {
                    sessionCount = sessions.count
                }
            } catch {
                // Ignore - won't show session count
            }
        } else {
            // For hub mode, show node count instead
            do {
                let nodes = try await client.listNodes()
                await MainActor.run {
                    sessionCount = nil // Will use node count display below
                }
                let onlineCount = nodes.filter(\.isOnline).count
                await MainActor.run {
                    sessionCount = onlineCount // Reuse field for display
                }
            } catch {
                // Ignore
            }
        }
    }
}
