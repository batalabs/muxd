import SwiftUI

struct ConnectionView: View {
    @EnvironmentObject var appState: AppState
    @State private var showScanner = false
    @State private var showManual = false

    var body: some View {
        VStack(spacing: 32) {
            Spacer()

            // Logo/Title
            VStack(spacing: 8) {
                Image(systemName: "server.rack")
                    .font(.system(size: 64))
                    .foregroundColor(.accentColor)

                Text("muxd")
                    .font(.largeTitle)
                    .fontWeight(.bold)

                Text("AI Coding Agent")
                    .font(.subheadline)
                    .foregroundColor(.secondary)
            }

            Spacer()

            // Connection options
            VStack(spacing: 16) {
                Button(action: { showScanner = true }) {
                    Label("Scan QR Code", systemImage: "qrcode.viewfinder")
                        .frame(maxWidth: .infinity)
                        .padding()
                        .background(Color.accentColor)
                        .foregroundColor(.white)
                        .cornerRadius(12)
                }

                Button(action: { showManual = true }) {
                    Label("Enter Manually", systemImage: "keyboard")
                        .frame(maxWidth: .infinity)
                        .padding()
                        .background(Color(.systemGray5))
                        .foregroundColor(.primary)
                        .cornerRadius(12)
                }
            }
            .padding(.horizontal, 32)

            Spacer()

            // Instructions
            Text("Run /qr in muxd to display the connection QR code")
                .font(.caption)
                .foregroundColor(.secondary)
                .multilineTextAlignment(.center)
                .padding(.horizontal)

            Spacer()
        }
        .sheet(isPresented: $showScanner) {
            QRScannerView { info in
                Task {
                    await appState.connect(with: info)
                }
            }
        }
        .sheet(isPresented: $showManual) {
            ManualConnectionView { info in
                Task {
                    await appState.connect(with: info)
                }
            }
        }
        .alert(item: $appState.error) { error in
            Alert(
                title: Text("Connection Error"),
                message: Text(error.localizedDescription),
                dismissButton: .default(Text("OK"))
            )
        }
        .task {
            // Try to restore previous connection
            await appState.restoreConnection()
        }
    }
}

