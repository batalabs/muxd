import SwiftUI

struct NodePickerView: View {
    @EnvironmentObject var appState: AppState
    @Binding var navigationPath: NavigationPath

    var body: some View {
        Group {
            if appState.hubNodes.isEmpty {
                ContentUnavailableView {
                    Label("No Nodes", systemImage: "server.rack")
                } description: {
                    Text("No daemon nodes registered with this hub")
                } actions: {
                    Button("Refresh") {
                        Task { await appState.refreshNodes() }
                    }
                    .buttonStyle(.borderedProminent)
                }
            } else {
                List {
                    Section {
                        ForEach(appState.hubNodes) { node in
                            Button {
                                if node.isOnline {
                                    appState.selectNode(node)
                                    navigationPath.append("sessions")
                                }
                            } label: {
                                NodeRowView(node: node)
                            }
                            .disabled(!node.isOnline)
                        }
                    } header: {
                        HStack {
                            Text("\(appState.hubNodes.count) nodes")
                            Spacer()
                            let onlineCount = appState.hubNodes.filter(\.isOnline).count
                            Text("\(onlineCount) online")
                                .foregroundColor(.green)
                        }
                        .textCase(nil)
                    }
                }
                .listStyle(.insetGrouped)
                .refreshable {
                    await appState.refreshNodes()
                }
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                hubHeader
            }
            ToolbarItem(placement: .topBarTrailing) {
                Button(role: .destructive) {
                    appState.disconnect()
                } label: {
                    Image(systemName: "xmark.circle")
                }
            }
        }
        .task {
            await appState.refreshNodes()
        }
    }

    private var hubHeader: some View {
        HStack(spacing: 6) {
            Image(systemName: "point.3.connected.trianglepath.dotted")
            if let info = appState.connectionInfo {
                Text(info.name != info.host ? info.name : info.host)
                    .lineLimit(1)
                    .truncationMode(.tail)
            } else {
                Text("Hub")
            }
            Text("Hub")
                .font(.caption2)
                .fontWeight(.semibold)
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(Color.orange)
                .foregroundColor(.white)
                .clipShape(Capsule())
        }
    }
}

struct NodeRowView: View {
    let node: HubNode

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "server.rack")
                .font(.title3)
                .foregroundColor(node.isOnline ? .accentColor : .secondary)
                .frame(width: 28)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(node.name)
                        .font(.headline)
                        .foregroundColor(node.isOnline ? .primary : .secondary)

                    statusBadge
                }

                HStack(spacing: 8) {
                    if !node.version.isEmpty {
                        Text("v\(node.version)")
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }

                    Text(node.lastSeenAt.relativeDisplay)
                        .font(.caption)
                        .foregroundColor(.secondary)
                }
            }

            Spacer()

            if node.isOnline {
                Image(systemName: "chevron.right")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
        .padding(.vertical, 4)
        .opacity(node.isOnline ? 1.0 : 0.6)
    }

    private var statusBadge: some View {
        Text(node.isOnline ? "Online" : "Offline")
            .font(.caption2)
            .fontWeight(.medium)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(node.isOnline ? Color.green.opacity(0.15) : Color.secondary.opacity(0.15))
            .foregroundColor(node.isOnline ? .green : .secondary)
            .clipShape(Capsule())
    }
}
