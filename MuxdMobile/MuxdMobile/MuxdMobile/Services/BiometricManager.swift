import LocalAuthentication
import SwiftUI
import Combine

@MainActor
class BiometricManager: ObservableObject {
    @Published var isLocked = false
    @Published var isAuthenticating = false

    @AppStorage("biometricLockEnabled") var biometricEnabled = false

    private let context = LAContext()

    var biometricType: LABiometryType {
        context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil)
        return context.biometryType
    }

    var biometricTypeName: String {
        switch biometricType {
        case .faceID: return "Face ID"
        case .touchID: return "Touch ID"
        case .opticID: return "Optic ID"
        default: return "Biometrics"
        }
    }

    var biometricIcon: String {
        switch biometricType {
        case .faceID: return "faceid"
        case .touchID: return "touchid"
        case .opticID: return "opticid"
        default: return "lock.fill"
        }
    }

    var canUseBiometrics: Bool {
        context.canEvaluatePolicy(.deviceOwnerAuthenticationWithBiometrics, error: nil)
    }

    func lockIfEnabled() {
        if biometricEnabled {
            isLocked = true
        }
    }

    func authenticate() async -> Bool {
        guard canUseBiometrics else {
            isLocked = false
            return true
        }

        isAuthenticating = true
        defer { isAuthenticating = false }

        let context = LAContext()
        context.localizedCancelTitle = "Use Passcode"

        do {
            let success = try await context.evaluatePolicy(
                .deviceOwnerAuthenticationWithBiometrics,
                localizedReason: "Unlock muxd"
            )
            if success {
                isLocked = false
            }
            return success
        } catch {
            // Fall back to passcode
            return await authenticateWithPasscode()
        }
    }

    private func authenticateWithPasscode() async -> Bool {
        let context = LAContext()

        do {
            let success = try await context.evaluatePolicy(
                .deviceOwnerAuthentication,
                localizedReason: "Unlock muxd"
            )
            if success {
                isLocked = false
            }
            return success
        } catch {
            return false
        }
    }
}
