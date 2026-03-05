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
