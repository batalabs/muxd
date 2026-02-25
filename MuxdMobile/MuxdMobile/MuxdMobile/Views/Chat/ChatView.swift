import SwiftUI
import Combine

// Glass effect modifier with iOS 26+ liquid glass support
struct GlassInputModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 26.0, *) {
            content
                .glassEffect(.regular, in: .capsule)
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

struct ChatView: View {
    @EnvironmentObject var appState: AppState
    @StateObject private var viewModel: ChatViewModel
    @State private var inputText = ""
    @State private var showModelPicker = false
    @FocusState private var inputFocused: Bool

    let session: Session

    init(session: Session) {
        self.session = session
        _viewModel = StateObject(wrappedValue: ChatViewModel(sessionID: session.id))
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
        .navigationTitle(session.displayTitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Button(action: {}) {
                        Label("Branch", systemImage: "arrow.triangle.branch")
                    }
                    Button(action: { showModelPicker = true }) {
                        Label("Change Model", systemImage: "cpu")
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
            }
        }
        .sheet(isPresented: $showModelPicker) {
            ModelPickerView(currentModel: session.model) { modelID in
                Task {
                    await viewModel.setModel(modelID: modelID)
                }
            }
        }
        .sheet(item: $viewModel.pendingAsk) { ask in
            AskUserView(prompt: ask.prompt) { answer in
                viewModel.answerAsk(answer: answer)
            }
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

}

struct MessageBubbleView: View {
    let message: TranscriptMessage

    var body: some View {
        VStack(alignment: message.isUser ? .trailing : .leading, spacing: 4) {
            // Role label for assistant
            if !message.isUser {
                HStack(spacing: 4) {
                    Image(systemName: "sparkles")
                        .font(.caption2)
                    Text("Assistant")
                        .font(.caption2)
                        .fontWeight(.medium)
                }
                .foregroundColor(.secondary)
            }

            HStack {
                if message.isUser { Spacer(minLength: 60) }

                MarkdownText(message.textContent)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(message.isUser ? Color.accentColor : Color(.systemGray5))
                    .foregroundColor(message.isUser ? .white : .primary)
                    .cornerRadius(18)

                if !message.isUser { Spacer(minLength: 60) }
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
        }
        .frame(maxWidth: .infinity, alignment: message.isUser ? .trailing : .leading)
    }
}

struct MarkdownText: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        if let attributed = try? AttributedString(markdown: text) {
            Text(attributed)
        } else {
            Text(text)
        }
    }
}

struct StreamingTextView: View {
    let text: String

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 4) {
                HStack(spacing: 4) {
                    Image(systemName: "sparkles")
                        .font(.caption2)
                    Text("Assistant")
                        .font(.caption2)
                        .fontWeight(.medium)
                }
                .foregroundColor(.secondary)

                MarkdownText(text)
                    .textSelection(.enabled)
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
            Spacer(minLength: 60)
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
