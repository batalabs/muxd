import SwiftUI

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

struct MessageBubbleView: View {
    let message: TranscriptMessage
    let allMessages: [TranscriptMessage]
    @AppStorage("fontSize") private var fontSize: AppFontSize = .medium
    @AppStorage("showLinkPreviews") private var showLinkPreviews = true
    @AppStorage("showTools") private var showTools = true

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
                            .padding(.horizontal, 12)
                            .padding(.top, 6)
                            .padding(.bottom, 12)
                            .background(Color(.systemGray5))
                            .cornerRadius(8)
                            .shadow(color: .black.opacity(0.06), radius: 4, x: 0, y: 2)
                    }
                }

                // Link previews for assistant messages
                if showLinkPreviews && !isActualUserMessage && !hasToolResults {
                    let urls = Array(NSOrderedSet(array: extractURLs(from: message.textContent))) as! [URL]
                    if !urls.isEmpty {
                        VStack(alignment: .leading, spacing: 4) {
                            ForEach(Array(urls.prefix(3).enumerated()), id: \.offset) { _, url in
                                LinkPreviewCard(url: url)
                            }
                        }
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
                if showTools && !message.toolUseBlocks.isEmpty {
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
                if showTools {
                    ForEach(groupToolResults(enrichedToolResultBlocks)) { group in
                        ToolResultBlockView(block: group.block, count: group.count)
                    }
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

    private let collapseThreshold = 300
    private var isLong: Bool { text.count > collapseThreshold }

    var body: some View {
        VStack(alignment: .trailing, spacing: 4) {
            if isLong && !isExpanded {
                // Show truncated text with gray [truncated] label
                Text(truncatedAttributedText)
                    .font(.system(size: 17 * scale))
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color.accentColor)
                    .foregroundColor(.white)
                    .cornerRadius(8)
                    .shadow(color: .black.opacity(0.06), radius: 4, x: 0, y: 2)
            } else {
                Text(text)
                    .font(.system(size: 17 * scale))
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color.accentColor)
                    .foregroundColor(.white)
                    .cornerRadius(8)
                    .shadow(color: .black.opacity(0.06), radius: 4, x: 0, y: 2)
            }

            if isLong {
                Button {
                    withAnimation(.easeInOut(duration: 0.2)) { isExpanded.toggle() }
                } label: {
                    Text(isExpanded ? "Show less" : "Show more")
                        .font(.caption)
                        .foregroundColor(.accentColor)
                }
            }
        }
    }
    
    private var truncatedAttributedText: AttributedString {
        var attributed = AttributedString(String(text.prefix(collapseThreshold)))
        attributed.foregroundColor = .white
        var truncatedLabel = AttributedString(" [truncated]")
        truncatedLabel.foregroundColor = .secondary
        attributed.append(truncatedLabel)
        return attributed
    }
}


struct GroupedToolResult: Identifiable {
    let block: ContentBlock
    let count: Int
    var id: String { block.id }
}

/// Truncates text to a maximum number of lines, adding "[truncated]" if truncated
func truncateToLines(_ text: String, maxLines: Int) -> String {
    let lines = text.components(separatedBy: .newlines)
    if lines.count <= maxLines {
        return text
    }
    return lines.prefix(maxLines).joined(separator: "\n") + "\n[truncated]"
}

/// Returns attributed string with text truncated to max lines and gray "[truncated]" label
func truncatedAttributedResult(text: String, maxLines: Int) -> AttributedString {
    let lines = text.components(separatedBy: .newlines)
    if lines.count <= maxLines {
        return AttributedString(text)
    }
    
    var attributed = AttributedString(lines.prefix(maxLines).joined(separator: "\n"))
    // Add blank line then truncated label for spacing
    var truncatedLabel = AttributedString("\n\n[truncated]")
    truncatedLabel.foregroundColor = .secondary
    attributed.append(truncatedLabel)
    return attributed
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
    @State private var isExpanded = false

    private var hasContent: Bool {
        if block.isImageResult && block.imageData != nil {
            return true
        }
        if let result = block.toolResult, !result.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return true
        }
        return false
    }

    private var lineCount: Int {
        guard let result = block.toolResult?.trimmingCharacters(in: .whitespacesAndNewlines) else { return 0 }
        return result.components(separatedBy: .newlines).count
    }

    private var isLong: Bool {
        lineCount > 2
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
                    // Tool name badge
                    HStack {
                        HStack(spacing: 4) {
                            Image(systemName: "wrench.fill")
                                .font(.system(size: 10))
                            Text(block.toolName ?? "Tool")
                                .font(.system(size: 11, weight: .medium))
                        }
                        .foregroundColor(.secondary)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(Color(.systemGray5))
                        .cornerRadius(6)
                        
                        Spacer()
                    }
                    
                    // Result content - tappable to expand/collapse
                    Group {
                        if isLong && !isExpanded {
                            let lines = result.components(separatedBy: .newlines)
                            VStack(alignment: .leading, spacing: 0) {
                                ForEach(Array(lines.prefix(2).enumerated()), id: \.offset) { _, line in
                                    Text(line)
                                        .font(.system(size: 12, design: .monospaced))
                                        .lineLimit(1)
                                }
                                Text("")
                                    .font(.system(size: 12, design: .monospaced))
                                Text("[truncated]")
                                    .font(.system(size: 12, design: .monospaced))
                                    .foregroundColor(.secondary)
                            }
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(8)
                            .background(Color(.systemGray6))
                            .cornerRadius(8)
                        } else {
                            let trimmed = result.trimmingCharacters(in: .whitespacesAndNewlines)
                            ScrollView(.horizontal, showsIndicators: true) {
                                Text(trimmed)
                                    .font(.system(size: 12, design: .monospaced))
                                    .lineLimit(nil)
                                    .fixedSize(horizontal: true, vertical: false)
                            }
                            .padding(8)
                            .background(Color(.systemGray6))
                            .cornerRadius(8)
                            .textSelection(.enabled)
                        }
                    }
                    .contentShape(Rectangle())
                    .onTapGesture {
                        if isLong {
                            withAnimation(.easeInOut(duration: 0.2)) { isExpanded.toggle() }
                        }
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

// MARK: - Link Previews

func extractURLs(from text: String) -> [URL] {
    guard let detector = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue) else { return [] }
    let range = NSRange(text.startIndex..., in: text)
    let matches = detector.matches(in: text, range: range)
    return matches.compactMap { $0.url }.filter { $0.scheme == "http" || $0.scheme == "https" }
}

struct LinkPreviewCard: View {
    let url: URL

    private var domain: String {
        url.host?.replacingOccurrences(of: "www.", with: "") ?? url.absoluteString
    }

    private var faviconURL: URL? {
        URL(string: "https://www.google.com/s2/favicons?sz=32&domain=\(domain)")
    }

    var body: some View {
        Link(destination: url) {
            HStack(spacing: 10) {
                AsyncImage(url: faviconURL) { image in
                    image.resizable()
                        .aspectRatio(contentMode: .fit)
                } placeholder: {
                    Image(systemName: "globe")
                        .foregroundColor(.secondary)
                }
                .frame(width: 20, height: 20)
                .cornerRadius(4)

                VStack(alignment: .leading, spacing: 2) {
                    Text(domain)
                        .font(.caption)
                        .fontWeight(.medium)
                        .foregroundColor(.primary)
                    Text(url.path.isEmpty || url.path == "/" ? url.absoluteString : url.path)
                        .font(.caption2)
                        .foregroundColor(.secondary)
                        .lineLimit(1)
                }

                Spacer()

                Image(systemName: "arrow.up.right")
                    .font(.caption2)
                    .foregroundColor(.secondary)
            }
            .padding(10)
            .background(Color(.systemGray6))
            .cornerRadius(8)
        }
    }
}
