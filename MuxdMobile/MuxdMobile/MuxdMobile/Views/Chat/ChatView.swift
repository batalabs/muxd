import SwiftUI
import Combine
import MarkdownUI

// Glass effect modifier with iOS 26+ liquid glass support
struct GlassInputModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 26.0, *) {
            content
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 24))
        } else {
            content
                .background {
                    RoundedRectangle(cornerRadius: 24)
                        .fill(.ultraThinMaterial)
                        .overlay {
                            RoundedRectangle(cornerRadius: 24)
                                .stroke(Color(.separator), lineWidth: 0.5)
                        }
                }
        }
    }
}

struct ChatGlassModifier: ViewModifier {
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

struct ChatGlassButtonStyle: ButtonStyle {
    var circular: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .modifier(ChatGlassModifier(circular: circular))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

struct ChatView: View {
    @EnvironmentObject var appState: AppState
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: ChatViewModel
    @State private var inputText = ""
    @State private var showModelPicker = false
    @State private var showRenameSheet = false
    @State private var showDeleteConfirmation = false
    @State private var isStarred = false
    @State private var sessionTitle: String
    @State private var isReady = false
    @FocusState private var inputFocused: Bool

    let session: Session

    init(session: Session) {
        self.session = session
        _viewModel = StateObject(wrappedValue: ChatViewModel(sessionID: session.id))
        _sessionTitle = State(initialValue: session.displayTitle)
        _isStarred = State(initialValue: session.tags?.contains("starred") ?? false)
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    ForEach(Array(viewModel.messages.enumerated()), id: \.offset) { index, message in
                        MessageBubbleView(message: message)
                            .id(index)
                    }

                    // Streaming response
                    if viewModel.isStreaming && !viewModel.streamingText.isEmpty {
                        StreamingTextView(text: viewModel.streamingText)
                            .id("streaming")
                    }

                    // Active tools
                    ForEach(Array(viewModel.activeTools.values), id: \.name) { tool in
                        ToolCallView(tool: tool)
                    }
                }
                .padding()
            }
            .scrollDismissesKeyboard(.interactively)
            .defaultScrollAnchor(.bottom)
            .opacity(isReady ? 1 : 0)
            .onChange(of: viewModel.messages.count) { oldCount, newCount in
                // Only scroll when new messages are added (not on first load)
                if oldCount > 0 && newCount > oldCount {
                    withAnimation(.easeOut(duration: 0.2)) {
                        proxy.scrollTo(newCount - 1, anchor: .bottom)
                    }
                }
            }
            .onChange(of: viewModel.streamingText) { _, _ in
                if viewModel.isStreaming {
                    proxy.scrollTo("streaming", anchor: .bottom)
                }
            }
            .safeAreaInset(edge: .bottom) {
                // Input bar with glass effect on entire container
                VStack(spacing: 8) {
                    HStack(spacing: 12) {
                        TextField("Message", text: $inputText, axis: .vertical)
                            .textFieldStyle(.plain)
                            .font(.body)
                            .lineLimit(1...6)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 12)
                            .modifier(GlassInputModifier())
                            .focused($inputFocused)
                            .disabled(viewModel.isStreaming)
                            .onSubmit {
                                sendMessage()
                            }

                        Button(action: viewModel.isStreaming ? cancelMessage : sendMessage) {
                            Image(systemName: viewModel.isStreaming ? "stop.fill" : "arrow.up")
                                .font(.title3.weight(.semibold))
                                .foregroundColor(.white)
                                .frame(width: 36, height: 36)
                                .background(viewModel.isStreaming ? Color.red : Color.accentColor)
                                .cornerRadius(18)
                        }
                        .disabled(inputText.isEmpty && !viewModel.isStreaming)
                        .opacity(inputText.isEmpty && !viewModel.isStreaming ? 0.5 : 1)
                    }

                    // Token count badge
                    if viewModel.inputTokens > 0 || viewModel.outputTokens > 0 || viewModel.isStreaming {
                        HStack {
                            HStack(spacing: 4) {
                                if viewModel.isStreaming {
                                    ProgressView()
                                        .scaleEffect(0.5)
                                }
                                Text("\(viewModel.inputTokens) in / \(viewModel.outputTokens) out")
                                    .font(.caption2)
                                    .foregroundColor(.secondary)
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 4)
                            .background(.ultraThinMaterial)
                            .cornerRadius(12)
                            Spacer()
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
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
                    HStack(spacing: 6) {
                        if isStarred {
                            Image(systemName: "star.fill")
                                .foregroundColor(.yellow)
                                .font(.system(size: 12))
                        }
                        Text(sessionTitle)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                    .frame(maxWidth: 200)
                    .modifier(ChatGlassModifier())
                }
            }
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button(action: {}) {
                        Label("Branch", systemImage: "arrow.triangle.branch")
                    }
                    Button(action: { showModelPicker = true }) {
                        Label("Change Model", systemImage: "cpu")
                    }
                } label: {
                    Image(systemName: "ellipsis")
                        .font(.system(size: 17, weight: .semibold))
                }
                .buttonStyle(ChatGlassButtonStyle(circular: true))
            }
        }
        .sheet(isPresented: $showModelPicker) {
            ModelPickerView(currentModel: session.model) { modelID in
                Task {
                    await viewModel.setModel(modelID: modelID)
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
            // Small delay to let scroll position settle before showing
            try? await Task.sleep(nanoseconds: 50_000_000)
            withAnimation(.easeIn(duration: 0.15)) {
                isReady = true
            }
        }
    }

    private func sendMessage() {
        guard !inputText.isEmpty else { return }
        viewModel.submit(text: inputText)
        inputText = ""
    }

    private func cancelMessage() {
        viewModel.cancel()
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
            isStarred.toggle() // Revert on error
            viewModel.error = error
        }
    }
}

struct MessageBubbleView: View {
    let message: TranscriptMessage
    @AppStorage("fontSize") private var fontSize: AppFontSize = .medium

    private var hasVisibleContent: Bool {
        // Has text content
        if !message.textContent.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return true
        }
        // Has tool result blocks with actual content (these should show)
        for block in message.toolResultBlocks {
            if block.isImageResult && block.imageData != nil {
                return true
            }
            if let result = block.toolResult, !result.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return true
            }
        }
        // Don't show messages with only tool_use blocks (no text) - they're just indicators
        return false
    }

    // Tool results should always be left-aligned (system output), even though they're in "user" messages
    private var hasToolResults: Bool {
        message.toolResultBlocks.contains { block in
            (block.isImageResult && block.imageData != nil) ||
            (block.toolResult != nil && !block.toolResult!.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        }
    }

    // True user message = user text, not tool results
    private var isActualUserMessage: Bool {
        message.isUser && !hasToolResults
    }

    var body: some View {
        if hasVisibleContent {
            VStack(alignment: isActualUserMessage ? .trailing : .leading, spacing: 4) {
                // Role label for assistant - show muxd branding
                if !isActualUserMessage && !hasToolResults {
                    HStack(spacing: 4) {
                        Image("Logo")
                            .resizable()
                            .frame(width: 16, height: 16)
                            .cornerRadius(4)
                        Text("muxd")
                            .font(.caption2)
                            .fontWeight(.medium)
                    }
                    .foregroundColor(.secondary)
                }

                // Text content
                if !message.textContent.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    HStack {
                        if isActualUserMessage { Spacer(minLength: 60) }

                        MarkdownText(message.textContent, scale: fontSize.scale)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 10)
                            .background(isActualUserMessage ? Color.accentColor : Color(.systemGray5))
                            .foregroundColor(isActualUserMessage ? .white : .primary)
                            .cornerRadius(18)

                        if !isActualUserMessage { Spacer(minLength: 20) }
                    }
                }

                // Tool uses
                if !message.toolUseBlocks.isEmpty {
                    HStack(spacing: 8) {
                        ForEach(message.toolUseBlocks) { block in
                            HStack(spacing: 4) {
                                Image(systemName: "wrench.fill")
                                    .font(.caption2)
                                Text(block.toolName ?? "Tool")
                                    .font(.caption)
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(Color(.systemGray6))
                            .cornerRadius(8)
                        }
                    }
                    .foregroundColor(.secondary)
                }

                // Tool results with content
                ForEach(message.toolResultBlocks) { block in
                    ToolResultBlockView(block: block)
                }
            }
            .frame(maxWidth: .infinity, alignment: isActualUserMessage ? .trailing : .leading)
        }
    }
}

struct ToolResultBlockView: View {
    let block: ContentBlock

    private var hasContent: Bool {
        if block.isImageResult && block.imageData != nil {
            return true
        }
        if let result = block.toolResult, !result.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return true
        }
        return false
    }

    var body: some View {
        if hasContent {
            VStack(alignment: .leading, spacing: 4) {
                // Show image if it's an image result
                if block.isImageResult, let imageData = block.imageData, let uiImage = UIImage(data: imageData) {
                    Image(uiImage: uiImage)
                        .resizable()
                        .aspectRatio(contentMode: .fit)
                        .frame(maxWidth: 300, maxHeight: 400)
                        .cornerRadius(12)
                } else if let result = block.toolResult, !result.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    // Show text result
                    HStack(spacing: 4) {
                        Image(systemName: block.isError == true ? "xmark.circle.fill" : "checkmark.circle.fill")
                            .foregroundColor(block.isError == true ? .red : .green)
                            .font(.caption)
                        Text(block.toolName ?? "Result")
                            .font(.caption)
                            .fontWeight(.medium)
                        Text("done")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }

                    Text(result)
                        .font(.caption)
                        .padding(8)
                        .background(Color(.systemGray6))
                        .cornerRadius(8)
                }
            }
        }
    }
}

struct MarkdownText: View {
    let text: String
    let scale: CGFloat

    init(_ text: String, scale: CGFloat = 1.0) {
        self.text = text
        self.scale = scale
    }

    private var textSize: CGFloat {
        17 * scale  // Base size 17pt (body)
    }

    private var codeSize: CGFloat {
        13 * scale  // Base size 13pt (caption)
    }

    var body: some View {
        Markdown(text)
            .markdownTextStyle(\.text) {
                FontSize(textSize)
            }
            .markdownTextStyle(\.code) {
                FontSize(codeSize)
            }
            .markdownBlockStyle(\.codeBlock) { configuration in
                configuration.label
                    .font(.system(size: codeSize, design: .monospaced))
                    .padding(8)
                    .background(Color(.systemGray6))
                    .cornerRadius(6)
            }
    }
}

struct StreamingTextView: View {
    let text: String
    @AppStorage("fontSize") private var fontSize: AppFontSize = .medium

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 4) {
                    Image("Logo")
                        .resizable()
                        .frame(width: 16, height: 16)
                        .cornerRadius(4)
                    Text("muxd")
                        .font(.caption2)
                        .fontWeight(.medium)
                }
                .foregroundColor(.secondary)

                MarkdownText(text, scale: fontSize.scale)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color(.systemGray5))
                    .cornerRadius(18)

                HStack(spacing: 4) {
                    ProgressView()
                        .scaleEffect(0.6)
                    Text("Generating...")
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }
            Spacer(minLength: 20)
        }
    }
}

struct ToolCallView: View {
    let tool: ChatViewModel.ToolStatus

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 4) {
                    if tool.status == "running" {
                        ProgressView()
                            .scaleEffect(0.6)
                    } else {
                        Image(systemName: tool.isError ? "xmark.circle.fill" : "checkmark.circle.fill")
                            .foregroundColor(tool.isError ? .red : .green)
                            .font(.caption)
                    }

                    Text(tool.name)
                        .font(.caption)
                        .fontWeight(.medium)

                    Text(tool.status)
                        .font(.caption2)
                        .foregroundColor(.secondary)
                }

                if let result = tool.result, !result.isEmpty {
                    Text(result.prefix(200) + (result.count > 200 ? "..." : ""))
                        .font(.caption2)
                        .foregroundColor(.secondary)
                        .lineLimit(3)
                }
            }
            .padding(8)
            .background(Color(.systemGray6))
            .cornerRadius(8)

            Spacer()
        }
    }
}

struct ChatRenameView: View {
    @Environment(\.dismiss) private var dismiss
    let title: String
    let onRename: (String) -> Void

    @State private var newTitle: String = ""

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Title", text: $newTitle)
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
                        onRename(newTitle)
                        dismiss()
                    }
                    .disabled(newTitle.isEmpty)
                }
            }
            .onAppear {
                newTitle = title
            }
        }
    }
}

struct AskUserView: View {
    @Environment(\.dismiss) private var dismiss
    let prompt: String
    let onAnswer: (String) -> Void

    @State private var answer = ""

    var body: some View {
        NavigationStack {
            VStack(spacing: 16) {
                Text(prompt)
                    .padding()

                TextField("Your response", text: $answer, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .lineLimit(3...6)
                    .padding(.horizontal)

                Spacer()
            }
            .navigationTitle("Input Needed")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Skip") {
                        onAnswer("")
                        dismiss()
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Send") {
                        onAnswer(answer)
                        dismiss()
                    }
                    .disabled(answer.isEmpty)
                }
            }
        }
    }
}

struct ModelPickerView: View {
    @Environment(\.dismiss) private var dismiss
    let currentModel: String
    let onSelect: (String) -> Void

    @State private var customModelID = ""

    private let commonModels = [
        ("Kimi K2.5", "accounts/fireworks/models/kimi-k2p5"),
    ]

    var body: some View {
        NavigationStack {
            List {
                Section("Common Models") {
                    ForEach(commonModels, id: \.1) { name, modelID in
                        Button(action: {
                            onSelect(modelID)
                            dismiss()
                        }) {
                            HStack {
                                Text(name)
                                    .foregroundColor(.primary)
                                Spacer()
                                if currentModel == modelID {
                                    Image(systemName: "checkmark")
                                        .foregroundColor(.accentColor)
                                }
                            }
                        }
                    }
                }

                Section("Custom Model") {
                    TextField("Model ID", text: $customModelID)
                        .autocapitalization(.none)
                        .autocorrectionDisabled()

                    Button("Use Custom Model") {
                        onSelect(customModelID)
                        dismiss()
                    }
                    .disabled(customModelID.isEmpty)
                }
            }
            .navigationTitle("Change Model")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }
}

@MainActor
class ChatViewModel: ObservableObject {
    @Published var messages: [TranscriptMessage] = []
    @Published var streamingText = ""
    @Published var isStreaming = false
    @Published var activeTools: [String: ToolStatus] = [:]
    @Published var pendingAsk: AskUserPrompt?
    @Published var error: Error?
    @Published var inputTokens = 0
    @Published var outputTokens = 0

    var client: MuxdClient?
    var sseClient: SSEClient?

    let sessionID: String

    struct ToolStatus {
        let name: String
        var status: String
        var result: String?
        var isError: Bool
    }

    struct AskUserPrompt: Identifiable {
        let id: String
        let prompt: String
    }

    init(sessionID: String) {
        self.sessionID = sessionID
    }

    func loadMessages() async {
        guard let client = client else { return }

        do {
            messages = try await client.getMessages(sessionID: sessionID)
        } catch {
            self.error = error
        }
    }

    func submit(text: String) {
        guard let sseClient = sseClient else { return }

        streamingText = ""
        isStreaming = true
        activeTools = [:]

        setupSSEHandlers()
        sseClient.submit(sessionID: sessionID, text: text)
    }

    func cancel() {
        guard let client = client else { return }

        sseClient?.cancel()
        isStreaming = false

        Task {
            try? await client.cancel(sessionID: sessionID)
        }
    }

    func answerAsk(answer: String) {
        guard let client = client, let ask = pendingAsk else { return }

        Task {
            try? await client.sendAskResponse(sessionID: sessionID, askID: ask.id, answer: answer)
        }
        pendingAsk = nil
    }

    func setModel(modelID: String) async {
        guard let client = client else { return }

        do {
            try await client.setModel(sessionID: sessionID, label: modelID, modelID: modelID)
        } catch {
            self.error = error
        }
    }

    private func setupSSEHandlers() {
        sseClient?.onEvent = { [weak self] event in
            Task { @MainActor in
                self?.handleEvent(event)
            }
        }

        sseClient?.onComplete = { [weak self] in
            Task { @MainActor in
                self?.isStreaming = false
                await self?.loadMessages()
            }
        }

        sseClient?.onError = { [weak self] error in
            Task { @MainActor in
                self?.error = error
                self?.isStreaming = false
            }
        }
    }

    private func handleEvent(_ event: SSEEvent) {
        switch event.type {
        case .delta:
            if let text = event.deltaText {
                streamingText += text
            }

        case .toolStart:
            if let id = event.toolUseID, let name = event.toolName {
                activeTools[id] = ToolStatus(name: name, status: "running", result: nil, isError: false)
            }

        case .toolDone:
            if let id = event.toolUseID {
                activeTools[id]?.status = "done"
                activeTools[id]?.result = event.toolResult
                activeTools[id]?.isError = event.toolIsError ?? false
            }

        case .streamDone:
            inputTokens += event.inputTokens ?? 0
            outputTokens += event.outputTokens ?? 0

        case .askUser:
            if let id = event.askID, let prompt = event.askPrompt {
                pendingAsk = AskUserPrompt(id: id, prompt: prompt)
            }

        case .turnDone:
            isStreaming = false
            Task { await loadMessages() }

        case .error:
            if let msg = event.errorMsg {
                error = MuxdError.serverError(msg)
            }
            isStreaming = false

        default:
            break
        }
    }
}
