import SwiftUI

@main
struct MuxdMobileApp: App {
    @StateObject private var appState = AppState()
    @AppStorage("appTheme") private var appTheme: AppTheme = .system

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(appState)
                .preferredColorScheme(appTheme.colorScheme)
        }
    }
}

struct ContentView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        Group {
            if appState.isConnected {
                MainTabView()
            } else {
                ConnectionView()
            }
        }
    }
}

struct MainTabView: View {
    var body: some View {
        TabView {
            SessionListView()
                .tabItem {
                    Label("Sessions", systemImage: "list.bullet")
                }

            ConfigView()
                .tabItem {
                    Label("Settings", systemImage: "gear")
                }
        }
    }
}
