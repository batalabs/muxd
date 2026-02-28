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
    @AppStorage("appTheme") private var appTheme: AppTheme = .system
    @AppStorage("fontSize") private var fontSize: AppFontSize = .medium

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

                Section("About") {
                    LabeledContent("App Version", value: "1.0.0")
                    LabeledContent("muxd Mobile", value: "iOS Client")
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
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize)
            .navigationTitle("Settings")
        }
    }
}
