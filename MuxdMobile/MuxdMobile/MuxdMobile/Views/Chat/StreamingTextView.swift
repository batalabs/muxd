import SwiftUI

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

                Text(text)
                    .font(.system(size: fontSize.scale * 16))
                    .textSelection(.enabled)
                    .padding(.horizontal, 12)
                    .padding(.top, 6)
                    .padding(.bottom, 12)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.systemGray5))
                    .cornerRadius(8)
                    .shadow(color: .black.opacity(0.06), radius: 4, x: 0, y: 2)
                    .padding(.bottom, 2)
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
