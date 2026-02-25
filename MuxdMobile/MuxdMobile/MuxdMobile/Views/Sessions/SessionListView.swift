import SwiftUI
import Combine

struct SessionListView: View {
    @EnvironmentObject var appState: AppState
    @StateObject private var viewModel = SessionListViewModel()

    var body: some View {
        NavigationStack {
            Group {
                if viewModel.sessions.isEmpty && !viewModel.isLoading {
                    ContentUnavailableView {
                        Label("No Sessions", systemImage: "bubble.left.and.bubble.right")
                    } description: {
                        Text("Create a new session to get started")
                    } actions: {
                        Button("New Session") {
                            viewModel.showNewSession = true
                        }
                        .buttonStyle(.borderedProminent)
                    }
                } else {
                    List {
                        ForEach(Array(viewModel.sessions.enumerated()), id: \.element.id) { index, session in
                            NavigationLink(value: session) {
                                SessionRowView(session: session)
                            }
                            .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                            .listRowSeparator(index == 0 ? .hidden : .visible, edges: .top)
                            .contextMenu {
                                Button {
                                    viewModel.sessionToRename = session
                                } label: {
                                    Label("Rename", systemImage: "pencil")
                                }

                                Button(role: .destructive) {
                                    Task {
                                        await viewModel.deleteSession(session)
                                    }
                                } label: {
                                    Label("Delete", systemImage: "trash")
                                }
                            }
                        }
                        .onDelete { indexSet in
                            Task {
                                await viewModel.deleteSessions(at: indexSet)
                            }
                        }
                    }
                    .listStyle(.plain)
                    .refreshable {
                        await viewModel.loadSessions()
                    }
                }
            }
            .navigationTitle("Sessions")
            .toolbar {
                ToolbarItem(placement: .primaryAction) {
                    Button(action: { viewModel.showNewSession = true }) {
                        Image(systemName: "plus")
                    }
                }
                ToolbarItem(placement: .cancellationAction) {
                    Button(action: { appState.disconnect() }) {
                        Image(systemName: "rectangle.portrait.and.arrow.right")
                    }
                }
            }
            .navigationDestination(for: Session.self) { session in
                ChatView(session: session)
            }
            .sheet(isPresented: $viewModel.showNewSession) {
                NewSessionView { projectPath, modelID in
                    await viewModel.createSession(projectPath: projectPath, modelID: modelID)
                }
            }
            .sheet(item: $viewModel.sessionToRename) { session in
                RenameSessionView(session: session) { newTitle in
                    await viewModel.renameSession(session, title: newTitle)
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
            }
            .onChange(of: viewModel.needsReconnect) { _, needsReconnect in
                if needsReconnect {
                    appState.disconnect()
                }
            }
        }
    }
}

enum ModelProvider {
    case anthropic
    case openai
    case google
    case meta
    case mistral
    case fireworks
    case deepseek
    case other

    init(model: String) {
        let lowercased = model.lowercased()
        if lowercased.contains("claude") || lowercased.contains("anthropic") {
            self = .anthropic
        } else if lowercased.contains("gpt") || lowercased.contains("openai") || lowercased.contains("o1") || lowercased.contains("o3") {
            self = .openai
        } else if lowercased.contains("gemini") || lowercased.contains("google") {
            self = .google
        } else if lowercased.contains("llama") || lowercased.contains("meta") {
            self = .meta
        } else if lowercased.contains("mistral") || lowercased.contains("mixtral") {
            self = .mistral
        } else if lowercased.contains("fireworks") || lowercased.contains("kimi") {
            self = .fireworks
        } else if lowercased.contains("deepseek") {
            self = .deepseek
        } else {
            self = .other
        }
    }

    var color: Color {
        switch self {
        case .anthropic: return Color(red: 0.85, green: 0.65, blue: 0.5)  // Tan/beige
        case .openai: return Color(red: 0.0, green: 0.65, blue: 0.52)     // Teal green
        case .google: return Color(red: 0.26, green: 0.52, blue: 0.96)    // Blue
        case .meta: return Color(red: 0.0, green: 0.47, blue: 1.0)        // Facebook blue
        case .mistral: return Color(red: 1.0, green: 0.5, blue: 0.0)      // Orange
        case .fireworks: return Color(red: 0.93, green: 0.26, blue: 0.21) // Red
        case .deepseek: return Color(red: 0.4, green: 0.3, blue: 0.9)     // Purple
        case .other: return Color.gray
        }
    }

    var shortName: String? {
        switch self {
        case .anthropic: return "Anthropic"
        case .openai: return "OpenAI"
        case .google: return "Google"
        case .meta: return "Meta"
        case .mistral: return "Mistral"
        case .fireworks: return "Fireworks"
        case .deepseek: return "DeepSeek"
        case .other: return nil
        }
    }
}

struct ModelBadgeView: View {
    let model: String

    var body: some View {
        let provider = ModelProvider(model: model)

        Text(model)
            .font(.caption2)
            .fontWeight(.medium)
            .padding(.horizontal, 5)
            .padding(.vertical, 3)
            .background(provider.color.opacity(0.15))
            .foregroundColor(provider.color)
            .cornerRadius(6)
    }
}

struct SessionRowView: View {
    let session: Session

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(session.displayTitle)
                    .font(.headline)
                    .lineLimit(1)

                Spacer()

                Text(session.shortID)
                    .font(.caption)
                    .foregroundColor(.secondary)
                    .monospaced()
            }

            HStack {
                if !session.model.isEmpty {
                    ModelBadgeView(model: session.model)
                }

                Spacer()

                Text("\(session.messageCount) messages")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            if let tags = session.tags, !tags.isEmpty {
                Text(tags)
                    .font(.caption2)
                    .foregroundColor(.accentColor)
            }
        }
        .padding(.vertical, 4)
    }
}

@MainActor
class SessionListViewModel: ObservableObject {
    @Published var sessions: [Session] = []
    @Published var isLoading = false
    @Published var showNewSession = false
    @Published var sessionToRename: Session?
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
                sessions = try await client.listSessions(project: nil, limit: 50)
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
            print("SessionListViewModel: client is nil!")
            self.error = "Not connected to server"
            return
        }

        isLoading = true
        defer { isLoading = false }

        do {
            print("SessionListViewModel: creating session at \(projectPath)")
            let sessionID = try await client.createSession(projectPath: projectPath, modelID: modelID)
            print("SessionListViewModel: created session \(sessionID)")
            await loadSessions()
            showNewSession = false
        } catch {
            print("SessionListViewModel: create error: \(error)")
            self.error = error.localizedDescription
        }
    }

    func deleteSessions(at indexSet: IndexSet) async {
        guard let client = client else {
            print("SessionListViewModel: client is nil for delete!")
            self.error = "Not connected to server"
            return
        }

        for index in indexSet {
            let session = sessions[index]
            do {
                print("SessionListViewModel: deleting session \(session.id)")
                try await client.deleteSession(id: session.id)
                print("SessionListViewModel: deleted session \(session.id)")
            } catch {
                print("SessionListViewModel: delete error: \(error)")
                self.error = error.localizedDescription
                return
            }
        }

        // Remove from local list
        sessions.remove(atOffsets: indexSet)
    }

    func deleteSession(_ session: Session) async {
        guard let client = client else {
            self.error = "Not connected to server"
            return
        }

        do {
            try await client.deleteSession(id: session.id)
            sessions.removeAll { $0.id == session.id }
        } catch {
            self.error = error.localizedDescription
        }
    }

    func renameSession(_ session: Session, title: String) async {
        guard let client = client else {
            self.error = "Not connected to server"
            return
        }

        do {
            try await client.renameSession(sessionID: session.id, title: title)
            if let index = sessions.firstIndex(where: { $0.id == session.id }) {
                sessions[index].title = title
            }
            sessionToRename = nil
        } catch {
            self.error = error.localizedDescription
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
