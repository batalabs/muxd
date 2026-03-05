import SwiftUI

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
                        .fill(Color(.separator).opacity(0.5))
                        .frame(height: 0.5)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 12)
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
                var attr = AttributedString(inner)
                attr.font = Font(codeFont)
                attr.foregroundColor = Color(.systemOrange)
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

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.offset) { rowIdx, row in
                    GridRow {
                        ForEach(0..<columnCount, id: \.self) { colIdx in
                            let cell = colIdx < row.count ? row[colIdx] : ""
                            Text(Self.formatCell(cell, fontSize: fontSize, isHeader: rowIdx == 0))
                                .foregroundColor(rowIdx == 0 ? .primary : .primary.opacity(0.85))
                                .lineLimit(1)
                                .fixedSize(horizontal: true, vertical: false)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 6)
                                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
                                .background(rowIdx == 0 ? Color(.systemGray5).opacity(0.6) : (rowIdx % 2 == 0 ? Color(.systemGray6).opacity(0.3) : Color.clear))
                                .overlay(alignment: .trailing) {
                                    if colIdx < columnCount - 1 {
                                        Rectangle()
                                            .fill(Color(.separator).opacity(0.25))
                                            .frame(width: 0.5)
                                    }
                                }
                                .overlay(alignment: .bottom) {
                                    if rowIdx == 0 {
                                        Rectangle()
                                            .fill(Color(.separator))
                                            .frame(height: 1)
                                    }
                                }
                        }
                    }
                }
            }
            .textSelection(.enabled)
            .background(NoBounceHelper())
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
    private static func applyLineHeight(_ para: NSMutableParagraphStyle, font: UIFont, multiple: CGFloat = 1.2) {
        let lineHeight = font.lineHeight * multiple
        para.minimumLineHeight = lineHeight
        para.maximumLineHeight = lineHeight
    }

    /// Baseline offset to prevent top clipping with lineHeight > 1.0
    private static func baselineOffset(font: UIFont, multiple: CGFloat = 1.2) -> CGFloat {
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
                    case 1: return (fontSize * 1.28, .bold)
                    case 2: return (fontSize * 1.18, .bold)
                    case 3: return (fontSize * 1.08, .semibold)
                    default: return (fontSize, .semibold)
                    }
                }()
                let headingFont = UIFont.systemFont(ofSize: sz, weight: wt)
                let para = NSMutableParagraphStyle()
                applyLineHeight(para, font: headingFont, multiple: 1.3)
                if !isFirst {
                    para.paragraphSpacingBefore = hadBlankLine ? 18 : 10
                }
                para.paragraphSpacing = 4
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
                let bulletColor = depth == 0 ? UIColor.label : UIColor.secondaryLabel
                let bulletStr = NSMutableAttributedString(string: "\t\(bulletChar)\t", attributes: [
                    .font: baseFont,
                    .foregroundColor: bulletColor,
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
                applyLineHeight(para, font: baseFont, multiple: 1.25)
                if !isFirst {
                    para.paragraphSpacingBefore = hadBlankLine ? 10 : 4
                }
                para.tabStops = [NSTextTab(textAlignment: .left, location: 16)]
                para.headIndent = 16
                para.firstLineHeadIndent = 0
                // Thick left bar using a block character
                let bar = NSMutableAttributedString(string: "\u{2503}\t", attributes: [
                    .font: UIFont.systemFont(ofSize: fontSize * 0.9),
                    .foregroundColor: UIColor.systemBlue.withAlphaComponent(0.5),
                    .paragraphStyle: para,
                    .baselineOffset: baselineOffset(font: baseFont, multiple: 1.25)
                ])
                bar.append(inlineMarkdown(content, baseAttrs: [
                    .font: UIFont.italicSystemFont(ofSize: fontSize),
                    .foregroundColor: UIColor.secondaryLabel,
                    .paragraphStyle: para,
                    .baselineOffset: baselineOffset(font: baseFont, multiple: 1.25)
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
                let codeFontAdjusted = UIFont.monospacedSystemFont(ofSize: baseFont.pointSize * 0.9, weight: .regular)
                let isDark = (baseAttrs[.foregroundColor] as? UIColor) == UIColor.white
                let codeColor = isDark ? UIColor.systemYellow : UIColor.systemOrange
                var codeAttrs: [NSAttributedString.Key: Any] = [
                    .font: codeFontAdjusted,
                    .foregroundColor: codeColor,
                    .backgroundColor: UIColor.systemGray5
                ]
                if let ps = baseAttrs[.paragraphStyle] { codeAttrs[.paragraphStyle] = ps }
                if let bo = baseAttrs[.baselineOffset] { codeAttrs[.baselineOffset] = bo }
                result.append(NSAttributedString(string: inner, attributes: codeAttrs))
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
    @AppStorage("showCodeLanguage") private var showCodeLanguage = true

    private var isDark: Bool { colorScheme == .dark }

    // Always use dark-style background for code blocks (like GitHub/ChatGPT)
    private var headerBg: Color {
        isDark ? Color(white: 0.18) : Color(white: 0.15)
    }
    private var codeBg: Color {
        isDark ? Color(white: 0.12) : Color(white: 0.1)
    }

    private static func languageIcon(_ lang: String?) -> String {
        switch lang?.lowercased() {
        case "swift": return "swift"
        case "python", "py": return "text.word.spacing"
        case "javascript", "js", "typescript", "ts", "jsx", "tsx": return "curlybraces"
        case "html", "xml", "svg": return "chevron.left.forwardslash.chevron.right"
        case "css", "scss", "sass": return "paintbrush"
        case "json", "yaml", "yml", "toml": return "doc.text"
        case "bash", "sh", "zsh", "shell", "fish": return "terminal"
        case "sql": return "cylinder"
        case "go", "golang": return "g.circle"
        case "rust", "rs": return "r.circle"
        case "c", "cpp", "c++", "objc", "objective-c": return "c.circle"
        case "java", "kotlin", "kt": return "j.circle"
        case "ruby", "rb": return "diamond"
        case "php": return "p.circle"
        case "dockerfile", "docker": return "shippingbox"
        case "markdown", "md": return "text.quote"
        case "diff": return "plus.forwardslash.minus"
        default: return "chevron.left.forwardslash.chevron.right"
        }
    }

    private var highlightedCode: AttributedString {
        // Always highlight as dark since code blocks are always on dark bg
        SyntaxHighlighter.highlight(content, language: language, fontSize: fontSize, isDark: true)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header bar
            HStack {
                if showCodeLanguage {
                    HStack(spacing: 4) {
                        Image(systemName: Self.languageIcon(language))
                            .font(.system(size: 10))
                        Text(language ?? "code")
                            .font(.system(size: 11, weight: .medium, design: .monospaced))
                    }
                    .foregroundColor(Color(white: 0.55))
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
                        .font(.system(size: 13))
                        .foregroundColor(copied ? .green : Color(white: 0.55))
                        .frame(width: 28, height: 28)
                        .background(Color.white.opacity(0.08))
                        .clipShape(Circle())
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(headerBg)

            // Code content with line highlights
            let codeLines = content.components(separatedBy: "\n")
            let baseFont = UIFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
            // Calculate exact line height to match Text rendering
            let lineHeight = baseFont.lineHeight * 1.15  // Small multiplier for line spacing
            
            ZStack(alignment: .topLeading) {
                // Alternating row backgrounds fill full width
                VStack(spacing: 0) {
                    ForEach(Array(codeLines.enumerated()), id: \.offset) { idx, _ in
                        Rectangle()
                            .fill(idx % 2 == 0 ? Color.clear : Color.white.opacity(0.03))
                            .frame(maxWidth: .infinity)
                            .frame(height: lineHeight)
                    }
                }
                .padding(.vertical, 10)

                // Scrollable code + line numbers
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(alignment: .top, spacing: 0) {
                        // Line numbers
                        VStack(alignment: .trailing, spacing: 0) {
                            ForEach(Array(codeLines.enumerated()), id: \.offset) { idx, _ in
                                Text("\(idx + 1)")
                                    .font(Font(baseFont))
                                    .foregroundColor(Color(white: 0.35))
                                    .frame(height: lineHeight, alignment: .center)
                            }
                        }
                        .padding(.leading, 10)
                        .padding(.trailing, 6)
                        .padding(.vertical, 10)

                        // Separator
                        Rectangle()
                            .fill(Color(white: 0.25))
                            .frame(width: 0.5)
                            .padding(.vertical, 8)

                        // Code
                        Text(highlightedCode)
                            .font(Font(baseFont))
                            .lineSpacing(0)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(.horizontal, 10)
                            .padding(.vertical, 10)
                            .textSelection(.enabled)
                    }
                    .background(NoBounceHelper())
                }
            }
            .background(codeBg)
        }
        .frame(maxWidth: .infinity)
        .cornerRadius(10)
    }
}

struct NoBounceHelper: UIViewRepresentable {
    func makeUIView(context: Context) -> UIView {
        let view = UIView()
        view.isUserInteractionEnabled = false
        view.frame = .zero
        return view
    }

    func updateUIView(_ uiView: UIView, context: Context) {
        DispatchQueue.main.async {
            // Walk up to find the nearest UIScrollView (the horizontal one we're inside)
            var current: UIView? = uiView.superview
            while let view = current {
                if let sv = view as? UIScrollView {
                    sv.bounces = false
                    return
                }
                current = view.superview
            }
        }
    }
}
