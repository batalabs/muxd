import SwiftUI

struct ChatView: View {
    @EnvironmentObject var appState: AppState
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: ChatViewModel
    @State private var showRenameSheet = false
    @State private var showDeleteConfirmation = false
    @State private var isStarred = false
    @State private var sessionTitle: String
    @State private var isReady = false
    @State private var userHasScrolledUp = false
    @State private var contentOverflows = false
    @State private var contentHeight: CGFloat = 0
    @State private var frameHeight: CGFloat = 0
    @AppStorage("showTools") private var showTools = true

    let session: Session

    private var chatMenuLabel: some View {
        HStack(spacing: 6) {
            if isStarred {
                Image(systemName: "star.fill")
                    .foregroundColor(.yellow)
            }
            Text(sessionTitle)
                .lineLimit(1)
                .truncationMode(.tail)
        }
        .frame(maxWidth: 200)
    }

    private var streamingPhase: StreamingStatusView.StreamingPhase {
        if let running = viewModel.activeTools.values.first(where: { $0.status == "running" }) {
            return .running(tool: running.name, summary: running.inputSummary)
        }
        if !viewModel.streamingText.isEmpty {
            return .streaming
        }
        return .thinking
    }

    init(session: Session) {
        self.session = session
        _viewModel = StateObject(wrappedValue: ChatViewModel(sessionID: session.id))
        _sessionTitle = State(initialValue: session.title ?? "New Chat")
        _isStarred = State(initialValue: session.tags?.contains("starred") ?? false)
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    // Load older messages button at top
                    if viewModel.hasOlderMessages {
                        Button {
                            viewModel.loadOlderMessages()
                        } label: {
                            HStack {
                                Spacer()
                                HStack(spacing: 6) {
                                    Image(systemName: "arrow.up.circle")
                                    Text("Load older messages")
                                }
                                .font(.subheadline)
                                .foregroundColor(.secondary)
                                .padding(.horizontal, 16)
                                .padding(.vertical, 10)
                                .background(Color(.systemGray6))
                                .cornerRadius(20)
                                Spacer()
                            }
                        }
                        .padding(.bottom, 8)
                    }
                    
                    // Windowed messages for display
                    ForEach(Array(viewModel.visibleMessages.enumerated()), id: \.offset) { index, message in
                        MessageBubbleView(message: message, allMessages: viewModel.messages)
                            .id("visible_\(index)")
                            .transition(.asymmetric(
                                insertion: .opacity.combined(with: .move(edge: .bottom)),
                                removal: .opacity
                            ))
                    }

                    // Streaming response
                    if viewModel.isStreaming && !viewModel.streamingText.isEmpty {
                        StreamingTextView(text: viewModel.streamingText)
                            .id("streaming")
                            .transition(.opacity.combined(with: .move(edge: .bottom)))
                    }

                    // Active tools (grouped by name+status)
                    if showTools {
                        ForEach(groupActiveTools(Array(viewModel.activeTools.values)), id: \.tool.name) { group in
                            ToolCallView(tool: group.tool, count: group.count)
                                .transition(.opacity.combined(with: .scale(scale: 0.95)))
                        }
                    }

                    // CLI-style status indicator
                    if viewModel.isStreaming {
                        StreamingStatusView(status: streamingPhase)
                            .transition(.opacity)
                    }

                    // Invisible anchor at the very bottom
                    Color.clear.frame(height: 1).id("bottom")
                }
                .animation(.easeOut(duration: 0.25), value: viewModel.visibleMessages.count)
                .animation(.easeOut(duration: 0.2), value: viewModel.isStreaming)
                .padding()
                .background(
                    GeometryReader { geo in
                        Color.clear.preference(key: ContentHeightKey.self, value: geo.size.height)
                    }
                )
            }
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
            .scrollIndicators(.hidden)
            .scrollDismissesKeyboard(.interactively)
            .defaultScrollAnchor(.bottom)
            .opacity(isReady ? 1 : 0)
            .background(
                GeometryReader { geo in
                    Color.clear.preference(key: FrameHeightKey.self, value: geo.size.height)
                }
            )
            .onPreferenceChange(ContentHeightKey.self) { value in
                contentHeight = value
                contentOverflows = contentHeight > frameHeight
            }
            .onPreferenceChange(FrameHeightKey.self) { value in
                frameHeight = value
                contentOverflows = contentHeight > frameHeight
            }
            .onChange(of: viewModel.visibleMessages.count) { oldCount, newCount in
                if oldCount > 0 && newCount > oldCount && !userHasScrolledUp {
                    withAnimation(.easeOut(duration: 0.2)) {
                        proxy.scrollTo("bottom", anchor: .bottom)
                    }
                }
            }
            .onChange(of: viewModel.streamingText) { _, _ in
                if viewModel.isStreaming && !userHasScrolledUp {
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
            }
            .onChange(of: viewModel.isStreaming) { _, isStreaming in
                if !userHasScrolledUp {
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
            }
            .simultaneousGesture(
                DragGesture(minimumDistance: 10)
                    .onChanged { value in
                        if value.translation.height > 30 && !userHasScrolledUp {
                            withAnimation(.easeOut(duration: 0.2)) { userHasScrolledUp = true }
                        }
                    }
            )
            .safeAreaInset(edge: .bottom) {
                // ISOLATED INPUT BAR - typing here won't re-render messages
                ChatInputBar(
                    onSend: { text, images in
                        userHasScrolledUp = false
                        viewModel.submit(text: text, images: images)
                    },
                    onCancel: { viewModel.cancel() },
                    isStreaming: viewModel.isStreaming,
                    inputTokens: viewModel.inputTokens,
                    outputTokens: viewModel.outputTokens,
                    activeTools: viewModel.activeTools,
                    onScrollToBottom: {
                        withAnimation(.easeOut(duration: 0.3)) {
                            userHasScrolledUp = false
                            proxy.scrollTo("bottom", anchor: .bottom)
                        }
                    },
                    showScrollButton: userHasScrolledUp && contentOverflows
                )
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Menu {
                    Button {
                        showRenameSheet = true
                    } label: {
                        Label("Rename", systemImage: "pencil")
                    }

                    Button {
                        isStarred.toggle()
                        Task {
                            await toggleStar()
                        }
                    } label: {
                        Label(isStarred ? "Unstar" : "Star", systemImage: isStarred ? "star.fill" : "star")
                    }

                    Divider()

                    Button(role: .destructive) {
                        showDeleteConfirmation = true
                    } label: {
                        Label("Delete", systemImage: "trash")
                    }
                } label: {
                    chatMenuLabel
                }
            }
        }
        .sheet(isPresented: $showRenameSheet) {
            ChatRenameView(title: sessionTitle) { newTitle in
                Task {
                    await renameSession(newTitle)
                }
            }
        }
        .sheet(item: $viewModel.pendingAsk) { ask in
            AskUserView(prompt: ask.prompt) { answer in
                viewModel.answerAsk(answer: answer)
            }
        }
        .alert("Delete Session?", isPresented: $showDeleteConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Delete", role: .destructive) {
                Task {
                    await deleteSession()
                }
            }
        } message: {
            Text("This will permanently delete this session and all its messages.")
        }
        .alert("Error", isPresented: .constant(viewModel.error != nil)) {
            Button("OK") { viewModel.error = nil }
        } message: {
            Text(viewModel.error?.localizedDescription ?? "Unknown error")
        }
        .task {
            viewModel.client = appState.getClient()
            viewModel.sseClient = appState.getSSEClient()
            await viewModel.loadMessages()
            try? await Task.sleep(nanoseconds: 50_000_000)
            withAnimation(.easeIn(duration: 0.15)) {
                isReady = true
            }
        }
    }

    private func renameSession(_ newTitle: String) async {
        guard let client = appState.getClient() else { return }
        do {
            try await client.renameSession(sessionID: session.id, title: newTitle)
            sessionTitle = newTitle
        } catch {
            viewModel.error = error
        }
    }

    private func deleteSession() async {
        guard let client = appState.getClient() else { return }
        do {
            try await client.deleteSession(id: session.id)
            dismiss()
        } catch {
            viewModel.error = error
        }
    }

    private func toggleStar() async {
        guard let client = appState.getClient() else { return }
        let newTags = isStarred ? "starred" : ""
        do {
            try await client.setTags(sessionID: session.id, tags: newTags)
        } catch {
            isStarred.toggle()
            viewModel.error = error
        }
    }
}

private struct ContentHeightKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        value = max(value, nextValue())
    }
}

private struct FrameHeightKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        value = max(value, nextValue())
    }
}
