import SwiftUI

struct StreamingTextView: View {
    let text: String
    @AppStorage("fontSize") private var fontSize: AppFontSize = .medium

    /// Strip incomplete markdown syntax from the last line of streaming text
    private var sanitizedText: String {
        guard !text.isEmpty else { return text }
        var lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        guard !lines.isEmpty else { return text }

        let lastIdx = lines.count - 1
        var lastLine = lines[lastIdx]

        // 1. Last line is only heading markers + spaces → drop entirely
        let trimmed = lastLine.trimmingCharacters(in: .whitespaces)
        if !trimmed.isEmpty, trimmed.contains("#"),
           trimmed.allSatisfy({ $0 == "#" || $0 == " " }) {
            lines.removeLast()
            return lines.joined(separator: "\n")
        }

        // 1b. Incomplete list marker on last line (-, *, +, or "1." with no content)
        if trimmed == "-" || trimmed == "*" || trimmed == "+" {
            lines.removeLast()
            return lines.joined(separator: "\n")
        }
        if trimmed.hasSuffix("."), trimmed.dropLast().allSatisfy(\.isNumber), !trimmed.dropLast().isEmpty {
            lines.removeLast()
            return lines.joined(separator: "\n")
        }

        // 2. Strip trailing * characters (partial bold/italic marker being typed)
        while lastLine.last == "*" {
            lastLine.removeLast()
        }

        // 3. Strip unmatched ** on last line (opening bold with no close yet)
        //    "a**b" → 2 parts (1 separator = unmatched), "a**b**c" → 3 parts (2 = matched)
        let starParts = lastLine.components(separatedBy: "**")
        if starParts.count % 2 == 0, let range = lastLine.range(of: "**", options: .backwards) {
            lastLine = String(lastLine[..<range.lowerBound])
        }

        // 4. Strip unmatched single * (italic) — count * that aren't part of **
        let cleaned = lastLine.replacingOccurrences(of: "**", with: "")
        let singleStarCount = cleaned.filter({ $0 == "*" }).count
        if singleStarCount % 2 != 0, let range = lastLine.range(of: "*", options: .backwards) {
            lastLine = String(lastLine[..<range.lowerBound])
        }

        // 5. Strip trailing backticks (incomplete inline code)
        let backtickCount = lastLine.reversed().prefix(while: { $0 == "`" }).count
        if backtickCount > 0 && backtickCount < 3 {
            lastLine.removeLast(backtickCount)
        }

        // 6. Strip incomplete link syntax: [ without closing ]
        if let openBracket = lastLine.range(of: "[", options: .backwards) {
            let rest = String(lastLine[openBracket.lowerBound...])
            if !rest.contains("]") {
                lastLine = String(lastLine[..<openBracket.lowerBound])
            }
        }

        lines[lastIdx] = lastLine
        return lines.joined(separator: "\n")
    }

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

                MarkdownText(sanitizedText, scale: fontSize.scale)
                    .padding(.horizontal, 12)
                    .padding(.top, 6)
                    .padding(.bottom, 12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.systemGray5))
                    .cornerRadius(8)
                    .shadow(color: .black.opacity(0.06), radius: 4, x: 0, y: 2)
            }
            Spacer(minLength: 20)
        }
    }
}

/// CLI-style status indicator shown during streaming
struct StreamingStatusView: View {
    let status: StreamingPhase
    @State private var dotCount = 0
    @State private var timer: Timer?

    enum StreamingPhase: Equatable {
        case thinking
        case running(tool: String, summary: String?)
        case streaming

        var label: String {
            switch self {
            case .thinking: return "Thinking"
            case .running(let tool, let summary):
                if let s = summary {
                    return "Running \(tool) \(truncatePath(s, maxLength: 25))"
                }
                return "Running \(tool)"
            case .streaming: return "Generating"
            }
        }

        var icon: String {
            switch self {
            case .thinking: return "brain"
            case .running: return "gearshape"
            case .streaming: return "text.cursor"
            }
        }
    }

    private var dots: String {
        String(repeating: ".", count: dotCount + 1)
    }

    var body: some View {
        HStack {
            HStack(spacing: 6) {
                // Animated spinner dot
                MiniDotSpinner()

                Text("\(status.label)\(dots)")
                    .font(.system(size: 13, weight: .medium, design: .monospaced))
                    .foregroundColor(.secondary)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .background(Color(.systemGray6))
            .cornerRadius(8)

            Spacer()
        }
        .onAppear { startTimer() }
        .onDisappear { stopTimer() }
        .onChange(of: status) { _, _ in
            dotCount = 0
        }
    }

    private func startTimer() {
        timer = Timer.scheduledTimer(withTimeInterval: 0.5, repeats: true) { _ in
            dotCount = (dotCount + 1) % 3
        }
    }

    private func stopTimer() {
        timer?.invalidate()
        timer = nil
    }
}

/// Mimics the CLI's MiniDot spinner — now using the muxd logo
struct MiniDotSpinner: View {
    @State private var phase = 0.0

    var body: some View {
        Image("Logo")
            .resizable()
            .frame(width: 16, height: 16)
            .cornerRadius(4)
            .scaleEffect(0.8 + 0.2 * sin(phase))
            .opacity(0.7 + 0.3 * sin(phase))
            .onAppear {
                withAnimation(.easeInOut(duration: 0.8).repeatForever(autoreverses: true)) {
                    phase = .pi
                }
            }
    }
}
