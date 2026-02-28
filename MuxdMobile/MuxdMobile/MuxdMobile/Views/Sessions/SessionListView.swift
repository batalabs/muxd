import SwiftUI
import Combine

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
    @State private var showServerPanel = false
    @State private var serverModel = ""

    var body: some View {
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
                    ForEach(viewModel.sessions) { session in
                        ZStack(alignment: .leading) {
                            NavigationLink(value: session) {
                                EmptyView()
                            }
                            .opacity(0)

                            HStack {
                                SessionRowView(session: session)
                                Spacer()
                                Image(systemName: "chevron.right")
                                    .font(.system(size: 14, weight: .semibold))
                                    .foregroundColor(Color(.tertiaryLabel))
                            }
                        }
                        .listRowInsets(EdgeInsets(top: 8, leading: 16, bottom: 8, trailing: 16))
                        .listRowSeparator(session.id == viewModel.sessions.first?.id ? .hidden : .visible, edges: .top)
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
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Button {
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.85)) {
                        showServerPanel.toggle()
                    }
                    if showServerPanel {
                        Task {
                            await loadServerModel()
                        }
                    }
                } label: {
                    HStack(spacing: 6) {
                        Image(systemName: "server.rack")
                        Text(appState.connectionInfo?.name ?? appState.connectionInfo?.host ?? "Sessions")
                        Image(systemName: showServerPanel ? "chevron.up" : "chevron.down")
                            .font(.system(size: 10, weight: .bold))
                    }
                    .modifier(GlassModifier())
                }
            }
            ToolbarItem(placement: .primaryAction) {
                Button(action: { viewModel.showNewSession = true }) {
                    Image(systemName: "plus")
                        .font(.system(size: 17, weight: .semibold))
                }
                .buttonStyle(GlassButtonStyle(circular: true))
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
        .sheet(isPresented: $viewModel.showToken) {
            TokenView(token: appState.connectionInfo?.token ?? "")
        }
        .overlay {
            if viewModel.isLoading {
                ProgressView()
            }
        }
        .overlay(alignment: .top) {
            if showServerPanel, let info = appState.connectionInfo {
                VStack(spacing: 0) {
                    // Dismiss scrim
                    Color.black.opacity(0.001)
                        .frame(height: 0)

                    VStack(spacing: 0) {
                        // Header
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(info.name.isEmpty ? info.host : info.name)
                                    .font(.headline)
                                Text("\(info.host):\(String(info.port))")
                                    .font(.caption)
                                    .foregroundColor(.secondary)
                            }
                            Spacer()
                            Button {
                                withAnimation(.spring(response: 0.35, dampingFraction: 0.85)) {
                                    showServerPanel = false
                                }
                            } label: {
                                Image(systemName: "xmark.circle.fill")
                                    .font(.title3)
                                    .foregroundColor(.secondary)
                            }
                        }
                        .padding(.horizontal, 20)
                        .padding(.top, 16)
                        .padding(.bottom, 12)

                        Divider().padding(.horizontal, 16)

                        // Model
                        VStack(alignment: .leading, spacing: 6) {
                            Label("Model", systemImage: "cpu")
                                .font(.subheadline.weight(.medium))
                                .foregroundColor(.secondary)
                            Text(serverModel.isEmpty ? "Not set" : serverModel)
                                .font(.body.monospaced())
                                .foregroundColor(serverModel.isEmpty ? .secondary : .primary)
                                .lineLimit(1)
                        }
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.horizontal, 20)
                        .padding(.vertical, 12)

                        Divider().padding(.horizontal, 16)

                        // Actions
                        VStack(spacing: 0) {
                            Button {
                                viewModel.showToken = true
                                withAnimation(.spring(response: 0.35, dampingFraction: 0.85)) {
                                    showServerPanel = false
                                }
                            } label: {
                                Label("View Token", systemImage: "key")
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .padding(.horizontal, 20)
                                    .padding(.vertical, 12)
                            }
                            .foregroundColor(.primary)

                            Button {
                                UIPasteboard.general.string = "\(info.host):\(String(info.port))"
                                withAnimation(.spring(response: 0.35, dampingFraction: 0.85)) {
                                    showServerPanel = false
                                }
                            } label: {
                                Label("Copy Address", systemImage: "doc.on.doc")
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .padding(.horizontal, 20)
                                    .padding(.vertical, 12)
                            }
                            .foregroundColor(.primary)

                            Divider().padding(.horizontal, 16)

                            Button(role: .destructive) {
                                withAnimation(.spring(response: 0.35, dampingFraction: 0.85)) {
                                    showServerPanel = false
                                }
                                appState.disconnect()
                            } label: {
                                Label("Disconnect", systemImage: "xmark.circle")
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                    .padding(.horizontal, 20)
                                    .padding(.vertical, 12)
                            }
                        }
                        .padding(.bottom, 8)
                    }
                    .background {
                        if #available(iOS 26.0, *) {
                            RoundedRectangle(cornerRadius: 20)
                                .fill(.clear)
                                .glassEffect(.regular, in: .rect(cornerRadius: 20))
                        } else {
                            RoundedRectangle(cornerRadius: 20)
                                .fill(.ultraThinMaterial)
                        }
                    }
                    .padding(.horizontal, 8)

                    Spacer()
                }
                .background(Color.black.opacity(0.3).onTapGesture {
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.85)) {
                        showServerPanel = false
                    }
                })
                .transition(.move(edge: .top).combined(with: .opacity))
                .zIndex(10)
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
}


struct SessionRowView: View {
    let session: Session

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 12) {
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
                Text(session.updatedAt.relativeDisplay)
                    .font(.caption)
                    .foregroundColor(.secondary)

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
    @Published var showToken = false
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
            self.error = "Not connected to server"
            return
        }

        isLoading = true
        defer { isLoading = false }

        do {
            _ = try await client.createSession(projectPath: projectPath, modelID: modelID)
            await loadSessions()
            showNewSession = false
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
