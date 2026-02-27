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

enum AppFontSize: String, CaseIterable {
    case small = "Small"
    case medium = "Medium"
    case large = "Large"

    var scale: CGFloat {
        switch self {
        case .small: return 0.85
        case .medium: return 1.0
        case .large: return 1.15
        }
    }
}

struct ConfigView: View {
    @EnvironmentObject var appState: AppState
    @AppStorage("appTheme") private var appTheme: AppTheme = .system
    @AppStorage("fontSize") private var fontSize: AppFontSize = .medium
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

                    Picker("Font Size", selection: $fontSize) {
                        ForEach(AppFontSize.allCases, id: \.self) { size in
                            Text(size.rawValue).tag(size)
                        }
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

                Section("Support") {
                    Link(destination: URL(string: "https://www.muxd.sh/support")!) {
                        HStack {
                            Text("Get Help")
                            Spacer()
                            Image(systemName: "arrow.up.right")
                                .font(.caption)
                                .foregroundColor(.secondary)
                        }
                    }
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
