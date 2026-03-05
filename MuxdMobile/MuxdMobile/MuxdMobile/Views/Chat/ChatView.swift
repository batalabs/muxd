import SwiftUI
import Combine
import PhotosUI
import UniformTypeIdentifiers

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
    @State private var contentOverflows = false
    @State private var contentHeight: CGFloat = 0
    @State private var frameHeight: CGFloat = 0
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

    private var streamingPhase: StreamingStatusView.StreamingPhase {
        // Check if any tool is currently running
        if let running = viewModel.activeTools.values.first(where: { $0.status == "running" }) {
            return .running(tool: running.name, summary: running.inputSummary)
        }
        // Streaming text is arriving
        if !viewModel.streamingText.isEmpty {
            return .streaming
        }
        // Waiting for first response
        return .thinking
    }

    private var hasAttachments: Bool {
        !inputText.isEmpty || !imageAttachments.isEmpty || !fileAttachments.isEmpty
    }

    @ViewBuilder
    private func inputBarWithScroll(proxy: ScrollViewProxy) -> some View {
        VStack(spacing: 8) {
            // Scroll-to-bottom button above input row, right-aligned
            if userHasScrolledUp && contentOverflows {
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
            HStack(spacing: 12) {
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
                                    .font(.system(size: 18))
                                    .foregroundStyle(.white, Color.black.opacity(0.6))
                            }
                        }
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
                                .font(.system(size: 18))
                                .foregroundStyle(.white, Color.black.opacity(0.6))
                        }
                    }
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
                    ForEach(groupActiveTools(Array(viewModel.activeTools.values)), id: \.tool.name) { group in
                        ToolCallView(tool: group.tool, count: group.count)
                            .transition(.opacity.combined(with: .scale(scale: 0.95)))
                    }

                    // CLI-style status indicator
                    if viewModel.isStreaming {
                        StreamingStatusView(status: streamingPhase)
                            .transition(.opacity)
                    }

                    // Invisible anchor at the very bottom
                    Color.clear.frame(height: 1).id("bottom")
                }
                .animation(.easeOut(duration: 0.25), value: viewModel.messages.count)
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
            .onChange(of: viewModel.messages.count) { oldCount, newCount in
                if oldCount > 0 && newCount > oldCount && !userHasScrolledUp {
                    withAnimation(.easeOut(duration: 0.2)) {
                        proxy.scrollTo("bottom", anchor: .bottom)
                    }
                }
            }
            .onChange(of: viewModel.streamingText) { _, newValue in
                // Only scroll on meaningful chunks to avoid jitter
                if viewModel.isStreaming && !userHasScrolledUp && newValue.count % 50 < 5 {
                    proxy.scrollTo("bottom", anchor: .bottom)
                }
            }
            .onChange(of: viewModel.isStreaming) { _, isStreaming in
                // Scroll to bottom when streaming starts or ends
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
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
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
