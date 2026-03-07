import SwiftUI
import Combine

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
        var inputSummary: String?
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

    func submit(text: String, images: [ImageAttachment] = []) {
        guard let sseClient = sseClient else { return }

        // Build blocks for the user message preview
        var blocks: [ContentBlock] = []
        if !text.isEmpty {
            blocks.append(ContentBlock(type: "text", text: text))
        }
        for img in images {
            blocks.append(ContentBlock(
                type: "image",
                mediaType: img.mediaType,
                base64Data: img.data,
                imagePath: img.path
            ))
        }

        let userMessage = TranscriptMessage(
            role: "user",
            content: text,
            blocks: blocks.isEmpty ? nil : blocks
        )
        messages.append(userMessage)

        streamingText = ""
        isStreaming = true
        activeTools = [:]

        setupSSEHandlers()
        sseClient.submit(sessionID: sessionID, text: text, images: images)
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
            Task { @MainActor [weak self] in
                self?.handleEvent(event)
            }
        }

        sseClient?.onComplete = { [weak self] in
            Task { @MainActor [weak self] in
                self?.isStreaming = false
                self?.activeTools = [:]
                await self?.loadMessages()
            }
        }

        sseClient?.onError = { [weak self] error in
            Task { @MainActor [weak self] in
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
                activeTools[id] = ToolStatus(name: name, inputSummary: event.toolInputSummary, status: "running", result: nil, isError: false)
            }

        case .toolDone:
            if let id = event.toolUseID {
                activeTools[id]?.status = "done"
                activeTools[id]?.result = event.toolResult
                activeTools[id]?.isError = event.toolIsError ?? false
                UIImpactFeedbackGenerator(style: .soft).impactOccurred()
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
            activeTools = [:]
            UINotificationFeedbackGenerator().notificationOccurred(.success)
            Task { await loadMessages() }

        case .error:
            if let msg = event.errorMsg {
                // "agent is already running" means another client is using this session —
                // don't show an error, just reset streaming state and reload messages.
                if msg.contains("already running") {
                    isStreaming = false
                    Task { await loadMessages() }
                    return
                }
                error = MuxdError.serverError(msg)
            }
            isStreaming = false

        default:
            break
        }
    }
}
