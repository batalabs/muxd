import SwiftUI
import Combine

struct FlexibleMenuModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 26.0, *) {
            content.buttonSizing(.flexible)
        } else {
            content
        }
    }
}

struct GlassModifier: ViewModifier {
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



struct GlassButtonStyle: ButtonStyle {
    var circular: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .modifier(GlassModifier(circular: circular))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

struct SessionListView: View {
    @EnvironmentObject var appState: AppState
    @StateObject private var viewModel = SessionListViewModel()
    @State private var serverModel = ""
    @State private var isHealthy = true
    @State private var latencyMs: Int?

    private var menuLabel: some View {
        HStack(spacing: 6) {
            Image(systemName: "server.rack")
            if let info = appState.connectionInfo {
                Text(info.name != info.host ? info.name : info.host)
                    .lineLimit(1)
                    .truncationMode(.tail)
            } else {
                Text("Sessions")
            }
        }
    }

    /// Extracts short model name from full path (e.g., "accounts/fireworks/models/gpt-4" → "gpt-4")
    private var shortModelName: String {
        guard !serverModel.isEmpty else { return "" }
        // Get last path component after "models/" or just last component
        if let modelsRange = serverModel.range(of: "models/") {
            return String(serverModel[modelsRange.upperBound...])
        }
        return serverModel.components(separatedBy: "/").last ?? serverModel
    }

    var body: some View {
        Group {
            if viewModel.sessions.isEmpty && !viewModel.isLoading {
                ContentUnavailableView {
                    Label("No Sessions", systemImage: "bubble.left.and.bubble.right")
                } description: {
                    Text("Create a new session to get started")
                } actions: {
                    Button("New Session") {
                        Task {
                            await viewModel.createSession(projectPath: "", modelID: nil)
                        }
                    }
                    .buttonStyle(.borderedProminent)
                }
            } else {
                List {
                    ForEach(viewModel.sessions) { session in
                        NavigationLink(value: session) {
                            SessionRowView(session: session, isNew: session.messageCount == 0)
                        }
                        .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                        .listRowSeparator(.hidden)
                        .listRowBackground(Color.clear)
                        .contextMenu {
                            Button {
                                Task {
                                    await viewModel.toggleStar(session)
                                }
                            } label: {
                                let isStarred = session.tags?.contains("starred") ?? false
                                Label(isStarred ? "Unstar" : "Star", systemImage: isStarred ? "star.fill" : "star")
                            }

                            Button {
                                viewModel.sessionToRename = session
                            } label: {
                                Label("Rename", systemImage: "pencil")
                            }

                            Button(role: .destructive) {
                                Task {
                                    await viewModel.deleteSessionByID(session.id)
                                }
                            } label: {
                                Label("Delete", systemImage: "trash")
                            }
                        }
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            Button("Delete", systemImage: "trash", role: .destructive) {
                                Task {
                                    await viewModel.deleteSessionByID(session.id)
                                }
                            }
                            .labelStyle(.iconOnly)
                        }
                    }
                }
                .listStyle(.plain)
                .scrollIndicators(.hidden)
                .refreshable {
                    await viewModel.loadSessions()
                }
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Menu {
                    if let info = appState.connectionInfo {
                        Section {
                            Button {
                                UIPasteboard.general.string = "\(info.host):\(String(info.port))"
                            } label: {
                                Label(info.host, systemImage: "server.rack")
                            }
                            .task { await checkHealth() }
                            Label("\(String(info.port))", systemImage: "number")
                            if !shortModelName.isEmpty {
                                Button {
                                    UIPasteboard.general.string = serverModel
                                } label: {
                                    Label(shortModelName, systemImage: "cpu")
                                }
                            }
                        }

                        Section {
                            Label(isHealthy ? "Connected" : "Disconnected", systemImage: isHealthy ? "circle.fill" : "circle")
                            if let ms = latencyMs {
                                Label("\(ms)ms", systemImage: "clock")
                            }
                        }

                        Section {
                            Button {
                                viewModel.showToken = true
                            } label: {
                                Label("View Token", systemImage: "key")
                            }
                        }

                        Section {
                            Button {
                                viewModel.showRenameConnection = true
                            } label: {
                                Label("Rename", systemImage: "character.cursor.ibeam")
                            }
                            Button(role: .destructive) {
                                appState.disconnect()
                            } label: {
                                Label("Disconnect", systemImage: "xmark.circle")
                            }
                        }
                    }
                } label: {
                    menuLabel
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    Task {
                        await viewModel.createSession(projectPath: "", modelID: nil)
                    }
                } label: {
                    Image(systemName: "plus")
                }
            }
        }
        .navigationDestination(for: Session.self) { session in
            ChatView(session: session)
        }
        .sheet(item: $viewModel.sessionToRename) { session in
            RenameSessionView(session: session) { newTitle in
                await viewModel.renameSession(session, title: newTitle)
            }
        }
        .sheet(isPresented: $viewModel.showToken) {
            TokenView(token: appState.connectionInfo?.token ?? "")
        }
        .sheet(isPresented: $viewModel.showRenameConnection) {
            if let info = appState.connectionInfo {
                RenameConnectionView(connection: info) { newName in
                    appState.renameConnection(id: info.id, name: newName)
                }
            }
        }
        .overlay {
            if viewModel.isLoading {
                ProgressView()
            }
        }
        .alert("Error", isPresented: Binding(
            get: { viewModel.error != nil },
            set: { if !$0 { viewModel.error = nil } }
        )) {
            Button("OK") { viewModel.error = nil }
        } message: {
            Text(viewModel.error ?? "Unknown error")
        }
        .task {
            viewModel.client = appState.getClient()
            await viewModel.loadSessions()
            await loadServerModel()
        }
        .onChange(of: viewModel.needsReconnect) { _, needsReconnect in
            if needsReconnect {
                appState.disconnect()
            }
        }
    }

    private func loadServerModel() async {
        guard let client = appState.getClient() else { return }
        do {
            let config = try await client.getConfig()
            if let model = config["model"] as? String {
                await MainActor.run { serverModel = model }
            }
        } catch {
            // Ignore — model field will show "Not set"
        }
    }

    private func checkHealth() async {
        guard let client = appState.getClient() else {
            await MainActor.run {
                isHealthy = false
                latencyMs = nil
            }
            return
        }
        do {
            let start = Date()
            let healthy = try await client.health()
            let elapsed = Date().timeIntervalSince(start)
            let ms = Int(elapsed * 1000)
            await MainActor.run {
                isHealthy = healthy
                latencyMs = ms
            }
        } catch {
            await MainActor.run {
                isHealthy = false
                latencyMs = nil
            }
        }
    }
}


struct SessionRowView: View {
    let session: Session
    var isNew: Bool = false

    private var isStarred: Bool {
        session.tags?.contains("starred") ?? false
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                if isStarred {
                    Image(systemName: "star.fill")
                        .font(.system(size: 12))
                        .foregroundColor(.yellow)
                } else if isNew {
                    Circle()
                        .fill(Color.accentColor)
                        .frame(width: 8, height: 8)
                }
                Text(session.displayTitle)
                    .font(.headline)
                    .lineLimit(1)

                Spacer(minLength: 8)

                Text(session.shortID)
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .monospaced()
            }

            HStack {
                Text(session.createdAt.fullDisplay)
                    .font(.caption)
                    .foregroundColor(.secondary)

                Spacer()

                Text("\(session.messageCount) messages")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .padding(.vertical, 4)
    }
}

@MainActor
class SessionListViewModel: ObservableObject {
    @Published var sessions: [Session] = []
    @Published var isLoading = false
    @Published var showToken = false
    @Published var showRenameConnection = false
    @Published var sessionToRename: Session?
    @Published var deleteSessionID: String?
    @Published var error: String?
    @Published var needsReconnect = false

    var client: MuxdClient?

    func loadSessions() async {
        guard let client = client else { return }

        isLoading = true
        defer { isLoading = false }

        // Retry up to 3 times with delay for server restarts
        for attempt in 1...3 {
            do {
                let fetchedSessions = try await client.listSessions(project: nil, limit: 50)
                // Sort with starred sessions at the top
                sessions = fetchedSessions.sorted { s1, s2 in
                    let s1Starred = s1.tags?.contains("starred") ?? false
                    let s2Starred = s2.tags?.contains("starred") ?? false
                    if s1Starred != s2Starred {
                        return s1Starred // starred sessions come first
                    }
                    return s1.updatedAt > s2.updatedAt // then by most recent
                }
                return
            } catch MuxdError.unauthorized {
                // Token is invalid - need to reconnect with new QR code
                needsReconnect = true
                return
            } catch {
                if attempt < 3 {
                    try? await Task.sleep(nanoseconds: 500_000_000) // 0.5s
                }
            }
        }
    }

    func createSession(projectPath: String, modelID: String?) async {
        guard let client = client else {
            self.error = "Not connected to server"
            return
        }

        isLoading = true
        defer { isLoading = false }

        do {
            _ = try await client.createSession(projectPath: projectPath, modelID: modelID)
            await loadSessions()
        } catch {
            self.error = error.localizedDescription
        }
    }

    func deleteSessions(at indexSet: IndexSet) async {
        guard let client = client else {
            self.error = "Not connected to server"
            return
        }

        for index in indexSet {
            let session = sessions[index]
            do {
                try await client.deleteSession(id: session.id)
            } catch {
                self.error = error.localizedDescription
                return
            }
        }

        sessions.remove(atOffsets: indexSet)
    }

    func deleteSessionByID(_ id: String) async {
        guard let client = client else {
            self.error = "Not connected to server"
            return
        }

        do {
            try await client.deleteSession(id: id)
            sessions.removeAll { $0.id == id }
            deleteSessionID = nil
        } catch {
            self.error = error.localizedDescription
            deleteSessionID = nil
        }
    }

    func renameSession(_ session: Session, title: String) async {
        guard let client = client else {
            self.error = "Not connected to server"
            return
        }

        do {
            try await client.renameSession(sessionID: session.id, title: title)
            await loadSessions()
            sessionToRename = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    func toggleStar(_ session: Session) async {
        guard let client = client else {
            self.error = "Not connected to server"
            return
        }

        let isCurrentlyStarred = session.tags?.contains("starred") ?? false
        let newTags = isCurrentlyStarred ? "" : "starred"

        do {
            try await client.setTags(sessionID: session.id, tags: newTags)
            await loadSessions()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct TokenView: View {
    @Environment(\.dismiss) private var dismiss
    let token: String
    @State private var showFullToken = false

    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                Spacer()

                Image(systemName: "key.fill")
                    .font(.system(size: 50))
                    .foregroundColor(.accentColor)

                Text("Connection Token")
                    .font(.title2)
                    .fontWeight(.semibold)

                if showFullToken {
                    Text(token)
                        .font(.system(.caption, design: .monospaced))
                        .padding()
                        .background(Color(.systemGray6))
                        .cornerRadius(8)
                        .textSelection(.enabled)
                        .padding(.horizontal)
                } else {
                    Button("Tap to reveal") {
                        showFullToken = true
                    }
                    .foregroundColor(.accentColor)
                }

                if showFullToken {
                    Button {
                        UIPasteboard.general.string = token
                    } label: {
                        Label("Copy Token", systemImage: "doc.on.doc")
                    }
                    .buttonStyle(.borderedProminent)
                }

                Spacer()

                Text("Keep this token secure. Anyone with access can connect to your server.")
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .multilineTextAlignment(.center)
                    .padding(.horizontal)
                    .padding(.bottom, 20)
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }
}

struct RenameSessionView: View {
    @Environment(\.dismiss) private var dismiss
    let session: Session
    let onRename: (String) async -> Void

    @State private var title: String = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Title", text: $title)
                        .autocapitalization(.sentences)
                } footer: {
                    Text("Enter a new title for this session")
                }
            }
            .navigationTitle("Rename Session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task {
                            await onRename(title)
                            dismiss()
                        }
                    }
                    .disabled(title.isEmpty)
                }
            }
            .onAppear {
                title = session.title
            }
        }
    }
}

struct NewSessionView: View {
    @Environment(\.dismiss) private var dismiss
    @State private var projectPath = ""
    @State private var modelID = ""

    let onCreate: (String, String?) async -> Void

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Project Path", text: $projectPath)
                        .autocapitalization(.none)
                } header: {
                    Text("Project")
                } footer: {
                    Text("Working directory for the session")
                }

                Section {
                    TextField("Model ID (optional)", text: $modelID)
                        .autocapitalization(.none)
                } footer: {
                    Text("Leave empty to use the default model")
                }
            }
            .navigationTitle("New Session")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Create") {
                        Task {
                            await onCreate(projectPath, modelID.isEmpty ? nil : modelID)
                        }
                    }
                    .disabled(projectPath.isEmpty)
                }
            }
        }
    }
}

