import Foundation
import LocalAuthentication
import os

enum DeviceAuthOutcome: Equatable, Sendable {
    case passed
    // A phone with no passcode has no owner check to pass, so the reveal goes ahead.
    case noPasscode
    case denied
}

protocol DeviceAuthenticating: Sendable {
    func authenticate(reason: String) async -> DeviceAuthOutcome
}

struct DeviceOwnerAuthenticator: DeviceAuthenticating {
    private static let log = Logger(subsystem: "org.pangeavpn", category: "auth")

    func authenticate(reason: String) async -> DeviceAuthOutcome {
        let context = LAContext()
        var unavailable: NSError?
        guard context.canEvaluatePolicy(.deviceOwnerAuthentication, error: &unavailable) else {
            return Self.outcome(failure: unavailable)
        }
        do {
            let passed = try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason)
            return passed ? .passed : .denied
        } catch {
            Self.log.info("owner check ended (\(error.logCode, privacy: .public))")
            return Self.outcome(failure: error)
        }
    }

    // Only a missing passcode lets the reveal through; a cancel, lockout or any other failure denies it.
    static func outcome(failure error: Error?) -> DeviceAuthOutcome {
        (error as? LAError)?.code == .passcodeNotSet ? .noPasscode : .denied
    }
}
