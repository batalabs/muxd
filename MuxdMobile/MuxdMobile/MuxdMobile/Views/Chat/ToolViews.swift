import SwiftUI

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
    @State private var isExpanded = false

    private var hasResult: Bool {
        if let result = tool.result, !result.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return true
        }
        return false
    }

    private var resultLineCount: Int {
        guard let result = tool.result?.trimmingCharacters(in: .whitespacesAndNewlines) else { return 0 }
        return result.components(separatedBy: .newlines).count
    }

    private var isLong: Bool {
        resultLineCount > 2
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            // Tool name badge
            HStack {
                HStack(spacing: 4) {
                    if tool.status == "running" {
                        ProgressView()
                            .scaleEffect(0.6)
                    } else {
                        Image(systemName: tool.isError ? "xmark.circle.fill" : "checkmark.circle.fill")
                            .foregroundColor(tool.isError ? .red : .green)
                            .font(.system(size: 10))
                    }
                    Text(tool.name)
                        .font(.system(size: 11, weight: .medium))
                    if let summary = tool.inputSummary {
                        Text(truncatePath(summary, maxLength: 30))
                            .font(.system(size: 11))
                            .foregroundColor(.secondary)
                            .lineLimit(1)
                    }
                    if count > 1 {
                        Text("(\(count))")
                            .font(.system(size: 10))
                            .foregroundColor(.secondary)
                    }
                }
                .foregroundColor(.secondary)
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(Color(.systemGray5))
                .cornerRadius(6)

                Spacer()
            }

            // Result content with truncation
            if hasResult, let result = tool.result {
                Group {
                    if isLong && !isExpanded {
                        let trimmed = result.trimmingCharacters(in: .whitespacesAndNewlines)
                        let lines = Array(trimmed.components(separatedBy: .newlines).filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }.prefix(2))
                        VStack(alignment: .leading, spacing: 0) {
                            ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                                Text(line)
                                    .font(.system(size: 12, design: .monospaced))
                                    .lineLimit(1)
                            }
                            Text("[truncated]")
                                .font(.system(size: 12, design: .monospaced))
                                .foregroundColor(.secondary)
                                .padding(.top, 2)
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
