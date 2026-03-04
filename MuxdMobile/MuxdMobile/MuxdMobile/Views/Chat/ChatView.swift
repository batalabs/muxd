import SwiftUI
import Combine
import PhotosUI
import UniformTypeIdentifiers

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

struct TintedGlassButtonStyle: ButtonStyle {
    var tint: Color = .accentColor

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.title3.weight(.semibold))
            .foregroundColor(.white)
            .frame(width: 44, height: 44)
            .background {
                if #available(iOS 26.0, *) {
                    Circle()
                        .fill(tint.opacity(0.8))
                        .glassEffect(.regular, in: .circle)
                } else {
                    Circle()
                        .fill(tint)
                        .overlay(Circle().stroke(Color.white.opacity(0.3), lineWidth: 1))
                }
            }
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

struct ImageAttachmentPreview: Identifiable {
    let id = UUID()
    let thumbnail: UIImage
    let attachment: ImageAttachment
}

struct FileAttachmentPreview: Identifiable {
    let id = UUID()
    let filename: String
    let content: String
}

struct ChatView: View {
    @EnvironmentObject var appState: AppState
    @Environment(\.dismiss) private var dismiss
    @StateObject private var viewModel: ChatViewModel
    @StateObject private var speechRecognizer = SpeechRecognizer()
    @State private var inputText = ""
    @State private var showRenameSheet = false
    @State private var showDeleteConfirmation = false
    @State private var isStarred = false
    @State private var sessionTitle: String
    @State private var isReady = false
    @State private var selectedPhotos: [PhotosPickerItem] = []
    @State private var imageAttachments: [ImageAttachmentPreview] = []
    @State private var fileAttachments: [FileAttachmentPreview] = []
    @State private var showFileImporter = false
    @State private var showPhotoPicker = false
    @State private var userHasScrolledUp = false
    @FocusState private var inputFocused: Bool

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

    private var hasAttachments: Bool {
        !inputText.isEmpty || !imageAttachments.isEmpty || !fileAttachments.isEmpty
    }

    @ViewBuilder
    private func inputBarWithScroll(proxy: ScrollViewProxy) -> some View {
        VStack(spacing: 8) {
            // Scroll-to-bottom button above input row, right-aligned
            if userHasScrolledUp {
                HStack {
                    Spacer()
                    Button {
                        withAnimation(.easeOut(duration: 0.3)) {
                            userHasScrolledUp = false
                            proxy.scrollTo("bottom", anchor: .bottom)
                        }
                    } label: {
                        Image(systemName: "arrow.down")
                            .font(.title3.weight(.semibold))
                            .foregroundColor(.accentColor)
                            .frame(width: 44, height: 44)
                            .background {
                                if #available(iOS 26.0, *) {
                                    Circle().fill(.clear)
                                        .glassEffect(.regular, in: .circle)
                                } else {
                                    Circle().fill(.ultraThinMaterial)
                                }
                            }
                    }
                }
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }

            // Attachment previews (images + files)
            if !imageAttachments.isEmpty || !fileAttachments.isEmpty {
                attachmentPreviews
            }

            HStack(spacing: 8) {
                // Text field with inline trailing button
                HStack(spacing: 8) {
                    TextField("Message", text: $inputText, axis: .vertical)
                        .textFieldStyle(.plain)
                        .font(.body)
                        .lineLimit(1...6)
                        .focused($inputFocused)
                        .disabled(viewModel.isStreaming || speechRecognizer.isRecording)
                        .onSubmit {
                            sendMessage()
                        }
                        .onChange(of: speechRecognizer.transcript) { _, newValue in
                            if !newValue.isEmpty {
                                inputText = newValue
                            }
                        }

                    // Inline trailing button: mic -> send arrow
                    if viewModel.isStreaming {
                        // Nothing inside the field while streaming
                    } else if hasAttachments {
                        Button(action: sendMessage) {
                            Image(systemName: "arrow.up.circle.fill")
                                .font(.title2)
                                .foregroundColor(.accentColor)
                        }
                    } else if speechRecognizer.isAuthorized {
                        Button {
                            speechRecognizer.toggleRecording()
                        } label: {
                            Image(systemName: speechRecognizer.isRecording ? "mic.fill" : "mic")
                                .font(.body)
                                .foregroundColor(speechRecognizer.isRecording ? .red : .secondary)
                        }
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .modifier(GlassInputModifier())

                if viewModel.isStreaming {
                    Button(action: cancelMessage) {
                        Image(systemName: "stop.fill")
                    }
                    .buttonStyle(TintedGlassButtonStyle(tint: .red))
                } else {
                    attachMenu
                }
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

    private var attachmentPreviews: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(imageAttachments) { preview in
                    Image(uiImage: preview.thumbnail)
                        .resizable()
                        .aspectRatio(contentMode: .fill)
                        .frame(width: 60, height: 60)
                        .clipShape(RoundedRectangle(cornerRadius: 8))
                        .overlay(alignment: .topTrailing) {
                            Button {
                                imageAttachments.removeAll { $0.id == preview.id }
                            } label: {
                                Image(systemName: "xmark.circle.fill")
                                    .font(.system(size: 16))
                                    .foregroundStyle(.white, Color.black.opacity(0.6))
                            }
                            .offset(x: 6, y: -6)
                        }
                        .padding(.top, 6)
                }
                ForEach(fileAttachments) { file in
                    VStack(spacing: 2) {
                        Image(systemName: "doc.text")
                            .font(.title3)
                        Text(file.filename)
                            .font(.system(size: 8))
                            .lineLimit(1)
                    }
                    .foregroundColor(.primary)
                    .frame(width: 60, height: 60)
                    .background(Color(.systemGray5))
                    .clipShape(RoundedRectangle(cornerRadius: 8))
                    .overlay(alignment: .topTrailing) {
                        Button {
                            fileAttachments.removeAll { $0.id == file.id }
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .font(.system(size: 16))
                                .foregroundStyle(.white, Color.black.opacity(0.6))
                        }
                        .offset(x: 6, y: -6)
                    }
                    .padding(.top, 6)
                }
            }
            .padding(.horizontal, 16)
        }
    }

    private var attachMenu: some View {
        Menu {
            Button {
                showPhotoPicker = true
            } label: {
                Label("Photo Library", systemImage: "photo.on.rectangle")
            }
            Button {
                showFileImporter = true
            } label: {
                Label("Files", systemImage: "paperclip")
            }
        } label: {
            Image(systemName: "plus")
                .font(.title3.weight(.semibold))
                .foregroundColor(.primary)
                .frame(width: 44, height: 44)
                .background {
                    if #available(iOS 26.0, *) {
                        Circle().fill(.clear)
                            .glassEffect(.regular, in: .circle)
                    } else {
                        Circle().fill(.ultraThinMaterial)
                    }
                }
        }
    }

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
                        MessageBubbleView(message: message, allMessages: viewModel.messages)
                            .id(index)
                    }

                    // Streaming response
                    if viewModel.isStreaming && !viewModel.streamingText.isEmpty {
                        StreamingTextView(text: viewModel.streamingText)
                            .id("streaming")
                    }

                    // Active tools (grouped by name+status)
                    ForEach(groupActiveTools(Array(viewModel.activeTools.values)), id: \.tool.name) { group in
                        ToolCallView(tool: group.tool, count: group.count)
                    }

                    // Invisible anchor at the very bottom
                    Color.clear.frame(height: 1).id("bottom")
                }
                .padding()
            }
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
            .scrollIndicators(.hidden)
            .scrollDismissesKeyboard(.interactively)
            .defaultScrollAnchor(.bottom)
            .opacity(isReady ? 1 : 0)
            .onChange(of: viewModel.messages.count) { oldCount, newCount in
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
            .simultaneousGesture(
                DragGesture(minimumDistance: 10)
                    .onChanged { value in
                        if value.translation.height > 30 && !userHasScrolledUp {
                            withAnimation(.easeOut(duration: 0.2)) { userHasScrolledUp = true }
                        }
                    }
            )
            .safeAreaInset(edge: .bottom) {
                inputBarWithScroll(proxy: proxy)
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
        .fileImporter(isPresented: $showFileImporter, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            handleImportedFiles(result)
        }
        .photosPicker(isPresented: $showPhotoPicker, selection: $selectedPhotos, maxSelectionCount: 5, matching: .images)
        .onChange(of: selectedPhotos) { _, _ in
            processSelectedPhotos()
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
        guard hasAttachments else { return }

        // Build the text: user input + file contents
        var fullText = inputText
        for file in fileAttachments {
            let fileBlock = "[\(file.filename)]\n```\n\(file.content)\n```"
            if fullText.isEmpty {
                fullText = fileBlock
            } else {
                fullText += "\n\n" + fileBlock
            }
        }

        let images = imageAttachments.map { $0.attachment }
        viewModel.submit(text: fullText, images: images)
        inputText = ""
        imageAttachments = []
        userHasScrolledUp = false
        fileAttachments = []
        selectedPhotos = []
    }

    private func handleImportedFiles(_ result: Result<[URL], Error>) {
        guard case .success(let urls) = result else { return }

        for url in urls {
            guard url.startAccessingSecurityScopedResource() else { continue }
            defer { url.stopAccessingSecurityScopedResource() }

            let filename = url.lastPathComponent
            let uti = UTType(filenameExtension: url.pathExtension)

            // Image files → route through image pipeline
            if let uti, uti.conforms(to: .image) {
                if let data = try? Data(contentsOf: url),
                   let uiImage = UIImage(data: data),
                   let prepared = ImageUtils.prepareForUpload(uiImage) {
                    let base64 = prepared.data.base64EncodedString()
                    let attachment = ImageAttachment(
                        path: filename,
                        mediaType: prepared.mediaType,
                        data: base64
                    )
                    imageAttachments.append(ImageAttachmentPreview(thumbnail: uiImage, attachment: attachment))
                }
                continue
            }

            // Text-based files → read content
            if let content = try? String(contentsOf: url, encoding: .utf8) {
                fileAttachments.append(FileAttachmentPreview(filename: filename, content: content))
            }
        }
    }

    private func processSelectedPhotos() {
        Task {
            var previews: [ImageAttachmentPreview] = []
            for item in selectedPhotos {
                guard let data = try? await item.loadTransferable(type: Data.self),
                      let uiImage = UIImage(data: data),
                      let prepared = ImageUtils.prepareForUpload(uiImage) else { continue }
                let base64 = prepared.data.base64EncodedString()
                let attachment = ImageAttachment(
                    path: "photo_\(UUID().uuidString.prefix(8)).jpg",
                    mediaType: prepared.mediaType,
                    data: base64
                )
                previews.append(ImageAttachmentPreview(thumbnail: uiImage, attachment: attachment))
            }
            imageAttachments = previews
        }
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
    let allMessages: [TranscriptMessage]
    @AppStorage("fontSize") private var fontSize: AppFontSize = .medium

    private var enrichedToolResultBlocks: [ContentBlock] {
        message.toolResultBlocksWithInput(from: allMessages)
    }

    private var hasVisibleContent: Bool {
        // Has text content
        if !message.textContent.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return true
        }
        // Has image blocks
        if !message.imageBlocks.isEmpty {
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
                    if isActualUserMessage {
                        HStack {
                            Spacer(minLength: 60)
                            CollapsibleUserMessage(text: message.textContent, scale: fontSize.scale)
                        }
                    } else {
                        MarkdownText(message.textContent, scale: fontSize.scale)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 10)
                            .background(Color(.systemGray5))
                            .foregroundColor(.primary)
                            .cornerRadius(18)
                    }
                }

                // Image blocks
                if !message.imageBlocks.isEmpty {
                    HStack {
                        if isActualUserMessage { Spacer(minLength: 60) }
                        VStack(alignment: isActualUserMessage ? .trailing : .leading, spacing: 4) {
                            ForEach(message.imageBlocks) { block in
                                ImageBlockView(block: block, isUserMessage: isActualUserMessage)
                            }
                        }
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

                // Tool results with content (grouped when consecutive identical)
                ForEach(groupToolResults(enrichedToolResultBlocks)) { group in
                    ToolResultBlockView(block: group.block, count: group.count)
                }
            }
            .frame(maxWidth: .infinity, alignment: isActualUserMessage ? .trailing : .leading)
        }
    }
}

struct CollapsibleUserMessage: View {
    let text: String
    let scale: CGFloat
    @State private var isExpanded = false

    /// Max lines before collapsing (approximate via character count)
    private let collapseThreshold = 300

    private var isLong: Bool {
        text.count > collapseThreshold
    }

    private var displayText: String {
        if isLong && !isExpanded {
            return String(text.prefix(collapseThreshold)) + "..."
        }
        return text
    }

    var body: some View {
        VStack(alignment: .trailing, spacing: 4) {
            Text(displayText)
                .font(.system(size: 17 * scale))
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .background(Color.accentColor)
                .foregroundColor(.white)
                .cornerRadius(18)

            if isLong {
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) {
                        isExpanded.toggle()
                    }
                } label: {
                    Text(isExpanded ? "Show less" : "Show more")
                        .font(.caption)
                        .foregroundColor(.accentColor)
                }
            }
        }
    }
}

struct GroupedToolResult: Identifiable {
    let block: ContentBlock
    let count: Int
    var id: String { block.id }
}

/// Truncates text to a maximum number of lines, adding "... (truncated)" if truncated
func truncateToLines(_ text: String, maxLines: Int) -> String {
    let lines = text.components(separatedBy: .newlines)
    if lines.count <= maxLines {
        return text
    }
    return lines.prefix(maxLines).joined(separator: "\n") + "\n... (truncated)"
}

/// Truncates path from the front, keeping the filename visible (e.g., "...internal/tools/tools.go")
func truncatePath(_ path: String, maxLength: Int) -> String {
    guard path.count > maxLength else { return path }
    let keepChars = maxLength - 3  // 3 for "..."
    return "..." + path.suffix(keepChars)
}

func groupToolResults(_ blocks: [ContentBlock]) -> [GroupedToolResult] {
    guard !blocks.isEmpty else { return [] }

    var groups: [GroupedToolResult] = []
    var i = 0

    while i < blocks.count {
        let current = blocks[i]
        var count = 1

        // Count consecutive blocks with the same toolName and toolResult
        while i + count < blocks.count {
            let next = blocks[i + count]
            let sameName = current.toolName == next.toolName
            let sameResult = current.toolResult == next.toolResult
            let sameError = current.isError == next.isError
            if sameName && sameResult && sameError {
                count += 1
            } else {
                break
            }
        }

        groups.append(GroupedToolResult(block: current, count: count))
        i += count
    }

    return groups
}

struct ImageBlockView: View {
    let block: ContentBlock
    var isUserMessage: Bool = false

    var body: some View {
        if let data = block.decodedImageData, let uiImage = UIImage(data: data) {
            VStack(alignment: isUserMessage ? .trailing : .leading, spacing: 4) {
                Image(uiImage: uiImage)
                    .resizable()
                    .aspectRatio(contentMode: .fit)
                    .frame(maxWidth: 280, maxHeight: 350)
                    .cornerRadius(12)
                if let path = block.imagePath {
                    Text(path)
                        .font(.caption2)
                        .foregroundColor(.secondary)
                        .multilineTextAlignment(isUserMessage ? .trailing : .leading)
                }
            }
        }
    }
}

struct ToolResultBlockView: View {
    let block: ContentBlock
    var count: Int = 1

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
                        if let summary = block.toolInputSummary {
                            Text(truncatePath(summary, maxLength: 30))
                                .font(.caption)
                                .foregroundColor(.secondary)
                                .lineLimit(1)
                        }
                        Text("done")
                            .font(.caption)
                            .foregroundColor(.secondary)
                        if count > 1 {
                            Text("(\(count))")
                                .font(.caption)
                                .foregroundColor(.secondary)
                        }
                        Spacer()
                    }

                    Text(truncateToLines(result, maxLines: 5))
                        .font(.caption)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(8)
                        .background(Color(.systemGray6))
                        .cornerRadius(8)
                        .textSelection(.enabled)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
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

    private var textSize: CGFloat { 17 * scale }
    private var codeSize: CGFloat { 13 * scale }

    private var segments: [MarkdownSegment] {
        MarkdownSegment.parse(text)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(Array(segments.enumerated()), id: \.offset) { _, segment in
                switch segment {
                case .text(let content):
                    SelectableMarkdownView(text: content, fontSize: textSize)
                        .fixedSize(horizontal: false, vertical: true)
                case .codeBlock(let language, let code):
                    CodeBlockView(content: code, language: language, fontSize: codeSize)
                case .horizontalRule:
                    Rectangle()
                        .fill(Color(.separator))
                        .frame(height: 1)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 8)
                case .table(let rows):
                    TableBlockView(rows: rows, fontSize: codeSize)
                }
            }
        }
    }
}

struct TableBlockView: View {
    let rows: [[String]]
    let fontSize: CGFloat
    @Environment(\.colorScheme) private var colorScheme

    private var columnCount: Int {
        rows.map(\.count).max() ?? 0
    }

    /// Strip inline markdown markers to get plain text for measuring
    private static func stripInlineMarkdown(_ text: String) -> String {
        var s = text
        // Bold-italic
        while let r = s.range(of: #"\*\*\*(.+?)\*\*\*"#, options: .regularExpression) {
            let inner = String(s[r]).dropFirst(3).dropLast(3)
            s.replaceSubrange(r, with: inner)
        }
        // Bold
        while let r = s.range(of: #"\*\*(.+?)\*\*"#, options: .regularExpression) {
            let inner = String(s[r]).dropFirst(2).dropLast(2)
            s.replaceSubrange(r, with: inner)
        }
        // Italic
        while let r = s.range(of: #"(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)"#, options: .regularExpression) {
            let inner = String(s[r]).dropFirst(1).dropLast(1)
            s.replaceSubrange(r, with: inner)
        }
        // Inline code
        while let r = s.range(of: #"`([^`]+)`"#, options: .regularExpression) {
            let inner = String(s[r]).dropFirst(1).dropLast(1)
            s.replaceSubrange(r, with: inner)
        }
        // Strikethrough
        while let r = s.range(of: #"~~(.+?)~~"#, options: .regularExpression) {
            let inner = String(s[r]).dropFirst(2).dropLast(2)
            s.replaceSubrange(r, with: inner)
        }
        return s
    }

    /// Build an inline-formatted AttributedString for a table cell
    private static func formatCell(_ text: String, fontSize: CGFloat, isHeader: Bool) -> AttributedString {
        var result = AttributedString()
        let baseFont = isHeader ? UIFont.systemFont(ofSize: fontSize, weight: .semibold) : UIFont.systemFont(ofSize: fontSize)
        let codeFont = UIFont.monospacedSystemFont(ofSize: fontSize * 0.9, weight: .regular)
        var remaining = text[text.startIndex...]

        let patterns: [(String, String)] = [
            (#"\*\*\*(.+?)\*\*\*"#, "boldItalic"),
            (#"\*\*(.+?)\*\*"#, "bold"),
            (#"~~(.+?)~~"#, "strikethrough"),
            (#"(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)"#, "italic"),
            (#"`([^`]+)`"#, "code"),
        ]

        while !remaining.isEmpty {
            var earliestRange: Range<String.Index>?
            var earliestType: String?
            for (pattern, type) in patterns {
                if let r = remaining.range(of: pattern, options: .regularExpression) {
                    if earliestRange == nil || r.lowerBound < earliestRange!.lowerBound {
                        earliestRange = r
                        earliestType = type
                    }
                }
            }
            guard let range = earliestRange, let type = earliestType else {
                var plain = AttributedString(String(remaining))
                plain.font = Font(baseFont)
                result.append(plain)
                break
            }
            let before = String(remaining[remaining.startIndex..<range.lowerBound])
            if !before.isEmpty {
                var plain = AttributedString(before)
                plain.font = Font(baseFont)
                result.append(plain)
            }
            let matched = String(remaining[range])
            switch type {
            case "boldItalic":
                let inner = String(matched.dropFirst(3).dropLast(3))
                var attr = AttributedString(inner)
                if let desc = baseFont.fontDescriptor.withSymbolicTraits([.traitBold, .traitItalic]) {
                    attr.font = Font(UIFont(descriptor: desc, size: baseFont.pointSize))
                } else {
                    attr.font = Font(UIFont.systemFont(ofSize: baseFont.pointSize, weight: .bold))
                }
                result.append(attr)
            case "bold":
                let inner = String(matched.dropFirst(2).dropLast(2))
                var attr = AttributedString(inner)
                attr.font = Font(UIFont.systemFont(ofSize: baseFont.pointSize, weight: .bold))
                result.append(attr)
            case "italic":
                let inner = String(matched.dropFirst(1).dropLast(1))
                var attr = AttributedString(inner)
                if let desc = baseFont.fontDescriptor.withSymbolicTraits(.traitItalic) {
                    attr.font = Font(UIFont(descriptor: desc, size: baseFont.pointSize))
                } else {
                    attr.font = Font(UIFont.italicSystemFont(ofSize: baseFont.pointSize))
                }
                result.append(attr)
            case "code":
                let inner = String(matched.dropFirst(1).dropLast(1))
                var attr = AttributedString("\u{2009}\(inner)\u{2009}")
                attr.font = Font(codeFont)
                attr.backgroundColor = Color(.systemGray5)
                result.append(attr)
            case "strikethrough":
                let inner = String(matched.dropFirst(2).dropLast(2))
                var attr = AttributedString(inner)
                attr.font = Font(baseFont)
                attr.strikethroughStyle = .single
                result.append(attr)
            default: break
            }
            remaining = remaining[range.upperBound...]
        }
        return result
    }

    /// Measure each column's ideal width based on stripped content
    private var columnWidths: [CGFloat] {
        guard columnCount > 0 else { return [] }
        let font = UIFont.systemFont(ofSize: fontSize)
        let boldFont = UIFont.systemFont(ofSize: fontSize, weight: .semibold)
        var widths = [CGFloat](repeating: 40, count: columnCount)
        for (rowIdx, row) in rows.enumerated() {
            for (colIdx, cell) in row.enumerated() where colIdx < columnCount {
                let measuringFont = rowIdx == 0 ? boldFont : font
                let plain = Self.stripInlineMarkdown(cell)
                let size = (plain as NSString).size(withAttributes: [.font: measuringFont])
                widths[colIdx] = max(widths[colIdx], ceil(size.width) + 20)
            }
        }
        return widths
    }

    var body: some View {
        let widths = columnWidths
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.offset) { rowIdx, row in
                    HStack(spacing: 0) {
                        ForEach(0..<columnCount, id: \.self) { colIdx in
                            let cell = colIdx < row.count ? row[colIdx] : ""
                            Text(Self.formatCell(cell, fontSize: fontSize, isHeader: rowIdx == 0))
                                .foregroundColor(rowIdx == 0 ? .primary : .primary.opacity(0.85))
                                .padding(.horizontal, 10)
                                .padding(.vertical, 6)
                                .frame(width: colIdx < widths.count ? widths[colIdx] : 40, alignment: .leading)
                        }
                    }
                    .background(rowIdx == 0 ? Color(.systemGray5).opacity(0.6) : (rowIdx % 2 == 0 ? Color(.systemGray6).opacity(0.3) : Color.clear))

                    if rowIdx == 0 {
                        Rectangle().fill(Color(.separator)).frame(height: 1)
                    }
                }
            }
            .textSelection(.enabled)
            .overlay(
                // Vertical column dividers
                HStack(spacing: 0) {
                    ForEach(0..<columnCount, id: \.self) { colIdx in
                        if colIdx < widths.count {
                            Spacer().frame(width: widths[colIdx])
                            if colIdx < columnCount - 1 {
                                Rectangle()
                                    .fill(Color(.separator).opacity(0.25))
                                    .frame(width: 0.5)
                            }
                        }
                    }
                }
            )
        }
        .background(Color(.systemGray6).opacity(0.4))
        .cornerRadius(8)
        .overlay(
            RoundedRectangle(cornerRadius: 8)
                .stroke(Color(.separator).opacity(0.3), lineWidth: 0.5)
        )
    }
}

struct SelectableMarkdownView: UIViewRepresentable {
    let text: String
    let fontSize: CGFloat

    func makeUIView(context: Context) -> UITextView {
        let textView = UITextView()
        textView.isEditable = false
        textView.isSelectable = true
        textView.isScrollEnabled = false
        textView.backgroundColor = .clear
        textView.textContainerInset = .zero
        textView.textContainer.lineFragmentPadding = 0
        textView.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        textView.setContentHuggingPriority(.required, for: .vertical)
        textView.linkTextAttributes = [
            .foregroundColor: UIColor.systemBlue,
            .underlineStyle: NSUnderlineStyle.single.rawValue
        ]
        return textView
    }

    func updateUIView(_ textView: UITextView, context: Context) {
        let isDark = textView.traitCollection.userInterfaceStyle == .dark
        let newAttr = MarkdownNSStringBuilder.build(text, fontSize: fontSize, isDark: isDark)
        if textView.attributedText.string != newAttr.string {
            textView.attributedText = newAttr
        }
    }

    @available(iOS 16.0, *)
    func sizeThatFits(_ proposal: ProposedViewSize, uiView: UITextView, context: Context) -> CGSize? {
        let maxWidth = UIScreen.main.bounds.width - 48
        let width: CGFloat
        if let pw = proposal.width, pw > 0, pw.isFinite {
            width = min(pw, maxWidth)
        } else {
            width = maxWidth
        }
        uiView.textContainer.size = CGSize(width: width, height: .greatestFiniteMagnitude)
        let size = uiView.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude))
        return CGSize(width: width, height: ceil(size.height))
    }
}

enum MarkdownNSStringBuilder {

    // MARK: - Line classification

    private enum LineType {
        case empty
        case heading(level: Int, content: String)
        case bullet(depth: Int, content: String)
        case numberedItem(depth: Int, number: String, content: String)
        case blockquote(content: String)
        case text(String)
    }

    private static func classify(_ line: String) -> LineType {
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        if trimmed.isEmpty { return .empty }

        // Headings
        if trimmed.hasPrefix("#### ") { return .heading(level: 4, content: String(trimmed.dropFirst(5))) }
        if trimmed.hasPrefix("### ") { return .heading(level: 3, content: String(trimmed.dropFirst(4))) }
        if trimmed.hasPrefix("## ") { return .heading(level: 2, content: String(trimmed.dropFirst(3))) }
        if trimmed.hasPrefix("# ") { return .heading(level: 1, content: String(trimmed.dropFirst(2))) }

        // Lists — detect nesting depth from leading spaces
        let leadingSpaces = line.prefix(while: { $0 == " " }).count
        let depth = leadingSpaces / 2
        if trimmed.hasPrefix("- ") || trimmed.hasPrefix("* ") {
            return .bullet(depth: depth, content: String(trimmed.dropFirst(2)))
        }

        if let match = trimmed.range(of: #"^\d+\.\s"#, options: .regularExpression) {
            let numPart = String(trimmed[trimmed.startIndex..<match.upperBound]).trimmingCharacters(in: .whitespaces)
            return .numberedItem(depth: depth, number: numPart, content: String(trimmed[match.upperBound...]))
        }

        // Blockquote
        if trimmed.hasPrefix("> ") { return .blockquote(content: String(trimmed.dropFirst(2))) }

        return .text(trimmed)
    }

    // MARK: - Build

    /// Apply line height to a paragraph style for readable spacing
    private static func applyLineHeight(_ para: NSMutableParagraphStyle, font: UIFont, multiple: CGFloat = 1.35) {
        let lineHeight = font.lineHeight * multiple
        para.minimumLineHeight = lineHeight
        para.maximumLineHeight = lineHeight
    }

    /// Baseline offset to prevent top clipping with lineHeight > 1.0
    private static func baselineOffset(font: UIFont, multiple: CGFloat = 1.35) -> CGFloat {
        (font.lineHeight * multiple - font.lineHeight) / 4
    }

    static func build(_ text: String, fontSize: CGFloat, isDark: Bool) -> NSAttributedString {
        let result = NSMutableAttributedString()
        let textColor = isDark ? UIColor.white : UIColor.black
        let baseFont = UIFont.systemFont(ofSize: fontSize)
        let codeFont = UIFont.monospacedSystemFont(ofSize: fontSize * 0.87, weight: .regular)
        let monoDigitFont = UIFont.monospacedDigitSystemFont(ofSize: fontSize, weight: .regular)
        let baseOffset = baselineOffset(font: baseFont)

        let lines = text.components(separatedBy: "\n")
        let classified = lines.map { classify($0) }

        var hadBlankLine = false
        var isFirst = true
        var prevWasList = false

        for lineType in classified {
            if case .empty = lineType { hadBlankLine = true; continue }

            let isList: Bool
            switch lineType {
            case .bullet, .numberedItem: isList = true
            default: isList = false
            }

            if !isFirst { result.append(NSAttributedString(string: "\n")) }

            switch lineType {
            case .heading(let level, let content):
                let (sz, wt): (CGFloat, UIFont.Weight) = {
                    switch level {
                    case 1: return (fontSize * 1.24, .bold)
                    case 2: return (fontSize * 1.14, .bold)
                    case 3: return (fontSize * 1.06, .semibold)
                    default: return (fontSize, .semibold)
                    }
                }()
                let headingFont = UIFont.systemFont(ofSize: sz, weight: wt)
                let para = NSMutableParagraphStyle()
                applyLineHeight(para, font: headingFont, multiple: 1.25)
                if !isFirst {
                    para.paragraphSpacingBefore = hadBlankLine ? 14 : 6
                }
                let attrs: [NSAttributedString.Key: Any] = [
                    .font: headingFont,
                    .foregroundColor: textColor,
                    .paragraphStyle: para,
                    .baselineOffset: baselineOffset(font: headingFont, multiple: 1.25)
                ]
                result.append(inlineMarkdown(content, baseAttrs: attrs, codeFont: codeFont))

            case .bullet(let depth, let content):
                // Tab-stop alignment: \t•\t pattern (Markdownosaur style)
                let nestIndent: CGFloat = CGFloat(depth) * 18
                let markerRight: CGFloat = 14 + nestIndent
                let contentStart: CGFloat = 18 + nestIndent

                let para = NSMutableParagraphStyle()
                applyLineHeight(para, font: baseFont)
                if !prevWasList && !isFirst {
                    para.paragraphSpacingBefore = hadBlankLine ? 8 : 3
                } else if prevWasList {
                    para.paragraphSpacingBefore = 2
                }
                para.tabStops = [
                    NSTextTab(textAlignment: .right, location: markerRight),
                    NSTextTab(textAlignment: .left, location: contentStart)
                ]
                para.defaultTabInterval = 28
                para.headIndent = contentStart
                para.firstLineHeadIndent = 0

                let bulletChar = depth == 0 ? "\u{2022}" : "\u{25E6}"
                let bulletStr = NSMutableAttributedString(string: "\t\(bulletChar)\t", attributes: [
                    .font: baseFont,
                    .foregroundColor: textColor,
                    .paragraphStyle: para,
                    .baselineOffset: baseOffset
                ])
                bulletStr.append(inlineMarkdown(content, baseAttrs: [
                    .font: baseFont,
                    .foregroundColor: textColor,
                    .paragraphStyle: para,
                    .baselineOffset: baseOffset
                ], codeFont: codeFont))
                result.append(bulletStr)

            case .numberedItem(let depth, let number, let content):
                // Tab-stop alignment: \t1.\t pattern — numbers right-align
                let nestIndent: CGFloat = CGFloat(depth) * 18
                let numberRight: CGFloat = 20 + nestIndent
                let contentStart: CGFloat = 24 + nestIndent

                let para = NSMutableParagraphStyle()
                applyLineHeight(para, font: baseFont)
                if !prevWasList && !isFirst {
                    para.paragraphSpacingBefore = hadBlankLine ? 8 : 3
                } else if prevWasList {
                    para.paragraphSpacingBefore = 2
                }
                para.tabStops = [
                    NSTextTab(textAlignment: .right, location: numberRight),
                    NSTextTab(textAlignment: .left, location: contentStart)
                ]
                para.defaultTabInterval = 28
                para.headIndent = contentStart
                para.firstLineHeadIndent = 0

                let numStr = NSMutableAttributedString(string: "\t\(number)\t", attributes: [
                    .font: monoDigitFont,
                    .foregroundColor: UIColor.secondaryLabel,
                    .paragraphStyle: para,
                    .baselineOffset: baseOffset
                ])
                numStr.append(inlineMarkdown(content, baseAttrs: [
                    .font: baseFont,
                    .foregroundColor: textColor,
                    .paragraphStyle: para,
                    .baselineOffset: baseOffset
                ], codeFont: codeFont))
                result.append(numStr)

            case .blockquote(let content):
                let para = NSMutableParagraphStyle()
                applyLineHeight(para, font: baseFont, multiple: 1.4)
                if !isFirst {
                    para.paragraphSpacingBefore = hadBlankLine ? 8 : 2
                }
                para.tabStops = [NSTextTab(textAlignment: .left, location: 14)]
                para.headIndent = 14
                para.firstLineHeadIndent = 0
                let bar = NSMutableAttributedString(string: "\u{258E}\t", attributes: [
                    .font: baseFont,
                    .foregroundColor: UIColor.separator,
                    .paragraphStyle: para,
                    .baselineOffset: baselineOffset(font: baseFont, multiple: 1.4)
                ])
                bar.append(inlineMarkdown(content, baseAttrs: [
                    .font: UIFont.italicSystemFont(ofSize: fontSize),
                    .foregroundColor: UIColor.secondaryLabel,
                    .paragraphStyle: para,
                    .baselineOffset: baselineOffset(font: baseFont, multiple: 1.4)
                ], codeFont: codeFont))
                result.append(bar)

            case .text(let content):
                let para = NSMutableParagraphStyle()
                applyLineHeight(para, font: baseFont)
                if !isFirst {
                    para.paragraphSpacingBefore = hadBlankLine ? 8 : 0
                }
                let attrs: [NSAttributedString.Key: Any] = [
                    .font: baseFont,
                    .foregroundColor: textColor,
                    .paragraphStyle: para,
                    .baselineOffset: baseOffset
                ]
                result.append(inlineMarkdown(content, baseAttrs: attrs, codeFont: codeFont))

            case .empty: break
            }

            prevWasList = isList
            hadBlankLine = false
            isFirst = false
        }

        return result
    }

    // MARK: - Inline markdown

    private struct InlineMatch {
        let range: Range<String.Index>
        let type: MatchType
        enum MatchType { case boldItalic, bold, italic, code, strikethrough, link }
    }

    private static func inlineMarkdown(_ text: String, baseAttrs: [NSAttributedString.Key: Any], codeFont: UIFont) -> NSAttributedString {
        let result = NSMutableAttributedString()
        let baseFont = baseAttrs[.font] as? UIFont ?? UIFont.systemFont(ofSize: 17)
        var remaining = text[text.startIndex...]

        let patterns: [(String, InlineMatch.MatchType)] = [
            (#"\*\*\*(.+?)\*\*\*"#, .boldItalic),
            (#"\*\*(.+?)\*\*"#, .bold),
            (#"__(.+?)__"#, .bold),
            (#"~~(.+?)~~"#, .strikethrough),
            (#"(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)"#, .italic),
            (#"(?<!_)_(?!_)(.+?)(?<!_)_(?!_)"#, .italic),
            (#"`([^`]+)`"#, .code),
            (#"\[([^\]]+)\]\(([^)]+)\)"#, .link)
        ]

        while !remaining.isEmpty {
            var earliest: InlineMatch?
            for (pattern, type) in patterns {
                if let r = remaining.range(of: pattern, options: .regularExpression) {
                    if earliest == nil || r.lowerBound < earliest!.range.lowerBound ||
                       (r.lowerBound == earliest!.range.lowerBound && r.upperBound > earliest!.range.upperBound) {
                        earliest = InlineMatch(range: r, type: type)
                    }
                }
            }

            guard let match = earliest else {
                result.append(NSAttributedString(string: String(remaining), attributes: baseAttrs))
                break
            }

            let before = String(remaining[remaining.startIndex..<match.range.lowerBound])
            if !before.isEmpty {
                result.append(NSAttributedString(string: before, attributes: baseAttrs))
            }

            let m = String(remaining[match.range])

            switch match.type {
            case .boldItalic:
                var attrs = baseAttrs
                if let desc = baseFont.fontDescriptor.withSymbolicTraits([.traitBold, .traitItalic]) {
                    attrs[.font] = UIFont(descriptor: desc, size: baseFont.pointSize)
                } else {
                    attrs[.font] = UIFont.systemFont(ofSize: baseFont.pointSize, weight: .bold)
                }
                result.append(inlineMarkdown(String(m.dropFirst(3).dropLast(3)), baseAttrs: attrs, codeFont: codeFont))
            case .bold:
                var attrs = baseAttrs
                let drop = m.hasPrefix("__") ? 2 : 2
                attrs[.font] = UIFont.systemFont(ofSize: baseFont.pointSize, weight: .bold)
                result.append(inlineMarkdown(String(m.dropFirst(drop).dropLast(drop)), baseAttrs: attrs, codeFont: codeFont))
            case .italic:
                var attrs = baseAttrs
                if let desc = baseFont.fontDescriptor.withSymbolicTraits(.traitItalic) {
                    attrs[.font] = UIFont(descriptor: desc, size: baseFont.pointSize)
                } else {
                    attrs[.font] = UIFont.italicSystemFont(ofSize: baseFont.pointSize)
                }
                result.append(inlineMarkdown(String(m.dropFirst(1).dropLast(1)), baseAttrs: attrs, codeFont: codeFont))
            case .code:
                let inner = String(m.dropFirst(1).dropLast(1))
                // Match baseline with surrounding text so inline code aligns vertically
                let codeFontAdjusted = UIFont.monospacedSystemFont(ofSize: baseFont.pointSize * 0.87, weight: .regular)
                let yOffset = (baseFont.capHeight - codeFontAdjusted.capHeight) / 2
                var codeAttrs: [NSAttributedString.Key: Any] = [
                    .font: codeFontAdjusted,
                    .foregroundColor: baseAttrs[.foregroundColor] ?? UIColor.label,
                    .backgroundColor: UIColor.systemGray5,
                    .baselineOffset: yOffset
                ]
                if let ps = baseAttrs[.paragraphStyle] { codeAttrs[.paragraphStyle] = ps }
                result.append(NSAttributedString(string: "\u{2009}\(inner)\u{2009}", attributes: codeAttrs))
            case .strikethrough:
                var attrs = baseAttrs
                attrs[.strikethroughStyle] = NSUnderlineStyle.single.rawValue
                result.append(inlineMarkdown(String(m.dropFirst(2).dropLast(2)), baseAttrs: attrs, codeFont: codeFont))
            case .link:
                if let bc = m.firstIndex(of: "]") {
                    let linkText = String(m[m.index(after: m.startIndex)..<bc])
                    let urlStr = String(m[m.index(bc, offsetBy: 2)..<m.index(before: m.endIndex)])
                    var attrs = baseAttrs
                    if let url = URL(string: urlStr) { attrs[.link] = url }
                    result.append(NSAttributedString(string: linkText, attributes: attrs))
                }
            }

            remaining = remaining[match.range.upperBound...]
        }

        return result
    }
}

enum MarkdownSegment {
    case text(String)
    case codeBlock(language: String?, code: String)
    case horizontalRule
    case table(rows: [[String]])

    private static func isHorizontalRule(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t == "---" || t == "***" || t == "___"
    }

    private static func isTableRow(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.hasPrefix("|") && t.hasSuffix("|") && t.count > 2
    }

    private static func isTableSeparator(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard t.hasPrefix("|") && t.contains("-") else { return false }
        let inner = String(t.dropFirst().dropLast())
        return inner.allSatisfy { $0 == "-" || $0 == "|" || $0 == ":" || $0 == " " }
    }

    private static func parseTableCells(_ line: String) -> [String] {
        line.trimmingCharacters(in: .whitespaces)
            .dropFirst().dropLast()
            .components(separatedBy: "|")
            .map { $0.trimmingCharacters(in: .whitespaces) }
    }

    private static func flushText(_ currentText: inout [String], into segments: inout [MarkdownSegment]) {
        let joined = currentText.joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        if !joined.isEmpty { segments.append(.text(joined)) }
        currentText = []
    }

    static func parse(_ text: String) -> [MarkdownSegment] {
        var segments: [MarkdownSegment] = []
        let lines = text.components(separatedBy: "\n")
        var currentText: [String] = []
        var inCodeBlock = false
        var codeLines: [String] = []
        var codeLanguage: String?
        var i = 0

        while i < lines.count {
            let line = lines[i]

            // Code block start
            if !inCodeBlock && line.hasPrefix("```") {
                flushText(&currentText, into: &segments)
                inCodeBlock = true
                codeLines = []
                let lang = String(line.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                codeLanguage = lang.isEmpty ? nil : lang
                i += 1; continue
            }
            // Code block end
            if inCodeBlock && line.hasPrefix("```") {
                segments.append(.codeBlock(language: codeLanguage, code: codeLines.joined(separator: "\n")))
                inCodeBlock = false; codeLines = []; codeLanguage = nil
                i += 1; continue
            }
            if inCodeBlock { codeLines.append(line); i += 1; continue }

            // Horizontal rule
            if isHorizontalRule(line) {
                flushText(&currentText, into: &segments)
                segments.append(.horizontalRule)
                i += 1; continue
            }

            // Table: collect consecutive | rows (skip separator rows)
            if isTableRow(line) {
                flushText(&currentText, into: &segments)
                var rows: [[String]] = []
                while i < lines.count && (isTableRow(lines[i]) || isTableSeparator(lines[i])) {
                    if !isTableSeparator(lines[i]) {
                        rows.append(parseTableCells(lines[i]))
                    }
                    i += 1
                }
                if !rows.isEmpty { segments.append(.table(rows: rows)) }
                continue
            }

            // Regular text
            currentText.append(line)
            i += 1
        }

        if inCodeBlock {
            segments.append(.codeBlock(language: codeLanguage, code: codeLines.joined(separator: "\n")))
        } else {
            flushText(&currentText, into: &segments)
        }

        return segments
    }
}


enum SyntaxHighlighter {
    // Common keywords across languages
    static let keywords = Set([
        // Swift/Kotlin
        "func", "let", "var", "if", "else", "for", "while", "return", "guard", "switch", "case", "default",
        "struct", "class", "enum", "protocol", "extension", "import", "private", "public", "internal",
        "static", "override", "final", "lazy", "weak", "unowned", "self", "super", "nil", "true", "false",
        "try", "catch", "throw", "throws", "async", "await", "in", "where", "as", "is", "init", "deinit",
        // JavaScript/TypeScript
        "const", "function", "new", "this", "typeof", "instanceof", "delete", "void", "undefined",
        "export", "from", "implements", "interface", "type", "declare", "module", "namespace",
        // Python
        "def", "elif", "except", "finally", "lambda", "pass", "raise", "with", "yield", "None", "True", "False",
        "and", "or", "not", "global", "nonlocal", "assert",
        // Go
        "package", "go", "chan", "select", "defer", "fallthrough", "range", "map", "make",
        // Rust
        "fn", "impl", "trait", "pub", "mod", "use", "crate", "mut", "ref", "move", "match", "loop",
        // General
        "break", "continue", "do"
    ])

    static let typeKeywords = Set([
        "String", "Int", "Bool", "Double", "Float", "Array", "Dictionary", "Set", "Optional",
        "Any", "AnyObject", "Void", "Never", "some", "any",
        "number", "string", "boolean", "object", "array", "null",
        "int", "float", "bool", "str", "list", "dict", "tuple"
    ])

    static func highlight(_ code: String, language: String?, fontSize: CGFloat, isDark: Bool) -> AttributedString {
        var result = AttributedString(code)
        let baseFont = UIFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)

        // Set base attributes
        result.font = baseFont
        result.foregroundColor = isDark ? .white : .black

        // Colors
        let keywordColor = isDark ? UIColor.systemPink : UIColor.systemPurple
        let stringColor = isDark ? UIColor.systemGreen : UIColor(red: 0.77, green: 0.1, blue: 0.08, alpha: 1)
        let commentColor = UIColor.systemGray
        let numberColor = isDark ? UIColor.systemYellow : UIColor.systemBlue
        let typeColor = isDark ? UIColor.systemCyan : UIColor.systemTeal

        // Highlight strings (double and single quoted)
        let stringPatterns = [
            "\"(?:[^\"\\\\]|\\\\.)*\"",  // Double quoted
            "'(?:[^'\\\\]|\\\\.)*'",      // Single quoted
            "`(?:[^`\\\\]|\\\\.)*`"       // Backtick (template literals)
        ]
        for pattern in stringPatterns {
            highlightPattern(pattern, in: &result, code: code, color: stringColor)
        }

        // Highlight comments
        highlightPattern("//[^\n]*", in: &result, code: code, color: commentColor)
        highlightPattern("#[^\n]*", in: &result, code: code, color: commentColor) // Python/Shell comments
        highlightPattern("/\\*[\\s\\S]*?\\*/", in: &result, code: code, color: commentColor) // Block comments

        // Highlight numbers
        highlightPattern("\\b\\d+\\.?\\d*\\b", in: &result, code: code, color: numberColor)

        // Highlight keywords
        for keyword in keywords {
            highlightPattern("\\b\(keyword)\\b", in: &result, code: code, color: keywordColor)
        }

        // Highlight type keywords
        for typeKw in typeKeywords {
            highlightPattern("\\b\(typeKw)\\b", in: &result, code: code, color: typeColor)
        }

        return result
    }

    private static func highlightPattern(_ pattern: String, in attributedString: inout AttributedString, code: String, color: UIColor) {
        guard let regex = try? NSRegularExpression(pattern: pattern, options: []) else { return }
        let nsRange = NSRange(code.startIndex..., in: code)
        let matches = regex.matches(in: code, options: [], range: nsRange)

        for match in matches {
            if let swiftRange = Range(match.range, in: code) {
                let start = AttributedString.Index(swiftRange.lowerBound, within: attributedString)
                let end = AttributedString.Index(swiftRange.upperBound, within: attributedString)
                if let start = start, let end = end {
                    attributedString[start..<end].foregroundColor = Color(color)
                }
            }
        }
    }
}

struct CodeBlockView: View {
    let content: String
    let language: String?
    let fontSize: CGFloat
    @State private var copied = false
    @Environment(\.colorScheme) private var colorScheme

    private var highlightedCode: AttributedString {
        SyntaxHighlighter.highlight(
            content,
            language: language,
            fontSize: fontSize,
            isDark: colorScheme == .dark
        )
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header with language and copy button
            HStack {
                if let language = language {
                    Text(language)
                        .font(.caption2)
                        .foregroundColor(.secondary)
                }
                Spacer()
                Button {
                    UIPasteboard.general.string = content
                    copied = true
                    let generator = UINotificationFeedbackGenerator()
                    generator.notificationOccurred(.success)
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
                        copied = false
                    }
                } label: {
                    Image(systemName: copied ? "checkmark.circle.fill" : "square.on.square")
                        .font(.system(size: 14))
                        .foregroundColor(copied ? .green : .secondary)
                        .frame(width: 28, height: 28)
                        .background(Color(.systemGray5))
                        .clipShape(Circle())
                }
            }
            .padding(.horizontal, 8)
            .padding(.top, 6)
            .padding(.bottom, 4)

            // Code content with syntax highlighting
            ScrollView(.horizontal, showsIndicators: false) {
                Text(highlightedCode)
                    .padding(.horizontal, 8)
                    .padding(.bottom, 8)
                    .textSelection(.enabled)
            }
        }
        .background(Color(.systemGray6))
        .cornerRadius(8)
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

struct GroupedActiveTool {
    let tool: ChatViewModel.ToolStatus
    let count: Int
}

func groupActiveTools(_ tools: [ChatViewModel.ToolStatus]) -> [GroupedActiveTool] {
    var groups: [String: GroupedActiveTool] = [:]
    var order: [String] = []

    for tool in tools {
        let key = "\(tool.name)_\(tool.status)"
        if let existing = groups[key] {
            groups[key] = GroupedActiveTool(tool: existing.tool, count: existing.count + 1)
        } else {
            groups[key] = GroupedActiveTool(tool: tool, count: 1)
            order.append(key)
        }
    }

    return order.compactMap { groups[$0] }
}

struct ToolCallView: View {
    let tool: ChatViewModel.ToolStatus
    var count: Int = 1

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

                    if let summary = tool.inputSummary {
                        Text(truncatePath(summary, maxLength: 40))
                            .font(.caption)
                            .foregroundColor(.secondary)
                            .lineLimit(1)
                    }

                    Text(tool.status)
                        .font(.caption2)
                        .foregroundColor(.secondary)

                    if count > 1 {
                        Text("(\(count))")
                            .font(.caption2)
                            .foregroundColor(.secondary)
                    }
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
