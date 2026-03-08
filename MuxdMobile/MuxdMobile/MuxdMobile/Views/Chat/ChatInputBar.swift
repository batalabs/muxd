import SwiftUI
import PhotosUI
import UniformTypeIdentifiers

struct ChatInputBar: View {
    let onSend: (String, [ImageAttachment]) -> Void
    let onCancel: () -> Void
    let isStreaming: Bool
    let inputTokens: Int
    let outputTokens: Int
    let activeTools: [String: ChatViewModel.ToolStatus]
    let onScrollToBottom: () -> Void
    let showScrollButton: Bool
    
    // Isolated state - changes here won't trigger parent re-renders
    @StateObject private var speechRecognizer = SpeechRecognizer()
    @State private var inputText = ""
    @State private var selectedPhotos: [PhotosPickerItem] = []
    @State private var imageAttachments: [ImageAttachmentPreview] = []
    @State private var fileAttachments: [FileAttachmentPreview] = []
    @State private var showFileImporter = false
    @State private var showPhotoPicker = false
    @FocusState private var inputFocused: Bool
    
    private var hasAttachments: Bool {
        !inputText.isEmpty || !imageAttachments.isEmpty || !fileAttachments.isEmpty
    }
    
    private var streamingPhase: StreamingStatusView.StreamingPhase {
        if let running = activeTools.values.first(where: { $0.status == "running" }) {
            return .running(tool: running.name, summary: running.inputSummary)
        }
        return .streaming
    }
    
    var body: some View {
        VStack(spacing: 8) {
            // Scroll-to-bottom button
            if showScrollButton {
                HStack {
                    Spacer()
                    Button {
                        onScrollToBottom()
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
            
            // Attachment previews
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
                        .disabled(isStreaming || speechRecognizer.isRecording)
                        .onSubmit {
                            sendMessage()
                        }
                        .onChange(of: speechRecognizer.transcript) { _, newValue in
                            if !newValue.isEmpty {
                                inputText = newValue
                            }
                        }
                    
                    // Inline trailing button: mic -> send arrow
                    if isStreaming {
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
                
                if isStreaming {
                    Button(action: onCancel) {
                        Image(systemName: "stop.fill")
                    }
                    .buttonStyle(TintedGlassButtonStyle(tint: .red))
                } else {
                    attachMenu
                }
            }
            
            // Token count badge
            if inputTokens > 0 || outputTokens > 0 || isStreaming {
                HStack {
                    HStack(spacing: 4) {
                        if isStreaming {
                            ProgressView()
                                .scaleEffect(0.5)
                        }
                        Text("\(inputTokens) in / \(outputTokens) out")
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
        .fileImporter(isPresented: $showFileImporter, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            handleImportedFiles(result)
        }
        .photosPicker(isPresented: $showPhotoPicker, selection: $selectedPhotos, maxSelectionCount: 5, matching: .images)
        .onChange(of: selectedPhotos) { _, _ in
            processSelectedPhotos()
        }
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
        }
        .buttonStyle(TintedGlassButtonStyle(tint: .accentColor))
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
        onSend(fullText, images)
        
        // Clear local state
        inputText = ""
        imageAttachments = []
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
}
