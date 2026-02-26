import SwiftUI

enum AppTheme: String, CaseIterable {
    case system = "System"
    case light = "Light"
    case dark = "Dark"

    var colorScheme: ColorScheme? {
        switch self {
        case .system: return nil
        case .light: return .light
        case .dark: return .dark
        }
    }
}

struct ConfigView: View {
    @EnvironmentObject var appState: AppState
    @AppStorage("appTheme") private var appTheme: AppTheme = .system
    @State private var config: [String: Any] = [:]
    @State private var isLoading = false
    @State private var error: String?

    var body: some View {
        NavigationStack {
            List {
                Section("Appearance") {
                    Picker("Theme", selection: $appTheme) {
                        ForEach(AppTheme.allCases, id: \.self) { theme in
                            Text(theme.rawValue).tag(theme)
                        }
                    }
                }

                Section("Connection") {
                    if appState.isConnected, let info = appState.connectionInfo {
                        LabeledContent("Server", value: info.name)
                        LabeledContent("Host", value: info.host)
                        LabeledContent("Port", value: "\(info.port)")
                        LabeledContent("Token", value: "••••••••")

                        if appState.savedConnections.count > 1 {
                            Button("Switch Server") {
                                appState.disconnect()
                            }
                        } else {
                            Button("Disconnect", role: .destructive) {
                                appState.disconnect()
                            }
                        }
                    } else {
                        Text("Not connected")
                            .foregroundColor(.secondary)
                    }
                }

                if !config.isEmpty {
                    Section("Server Config") {
                        if let model = config["model"] as? String, !model.isEmpty {
                            LabeledContent("Model", value: model)
                        }

                        if let footerTokens = config["footer_tokens"] as? Bool {
                            LabeledContent("Show Tokens", value: footerTokens ? "Yes" : "No")
                        }

                        if let footerCost = config["footer_cost"] as? Bool {
                            LabeledContent("Show Cost", value: footerCost ? "Yes" : "No")
                        }
                    }
                }

                Section("About") {
                    LabeledContent("App Version", value: "1.0.0")
                    LabeledContent("muxd Mobile", value: "iOS Client")
                }

                Section("Legal") {
                    Link(destination: URL(string: "https://www.muxd.sh/privacy")!) {
                        HStack {
                            Text("Privacy Policy")
                            Spacer()
                            Image(systemName: "arrow.up.right")
                                .font(.caption)
                                .foregroundColor(.secondary)
                        }
                    }

                    Link(destination: URL(string: "https://www.muxd.sh/terms")!) {
                        HStack {
                            Text("Terms of Service")
                            Spacer()
                            Image(systemName: "arrow.up.right")
                                .font(.caption)
                                .foregroundColor(.secondary)
                        }
                    }
                }
            }
            .navigationTitle("Settings")
            .refreshable {
                await loadConfig()
            }
            .overlay {
                if isLoading {
                    ProgressView()
                }
            }
            .alert("Error", isPresented: Binding(
                get: { error != nil },
                set: { if !$0 { error = nil } }
            )) {
                Button("OK") { error = nil }
            } message: {
                Text(error ?? "Unknown error")
            }
            .task {
                await loadConfig()
            }
        }
    }

    private func loadConfig() async {
        guard let client = appState.getClient() else { return }

        isLoading = true
        defer { isLoading = false }

        do {
            config = try await client.getConfig()
        } catch {
            // Silently fail - config is not critical
        }
    }
}
