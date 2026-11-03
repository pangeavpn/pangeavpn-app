import Foundation

// Keychain items outlive an uninstall but the app's own defaults do not, so a missing marker means a fresh install.
enum InstallMarker {
    static let key = "installMarker"

    enum Action: Equatable {
        case wipe
        case keep
    }

    static func action(markerPresent: Bool) -> Action {
        markerPresent ? .keep : .wipe
    }

    // The marker is only set once the wipe succeeds, so a failed wipe is retried on the next launch.
    static func apply(defaults: UserDefaults, wipe: () -> Bool) {
        guard action(markerPresent: defaults.bool(forKey: key)) == .wipe else { return }
        if wipe() { defaults.set(true, forKey: key) }
    }
}

extension GoCore {
    // App only: the wipe runs before Go first reads the Keychain, and the extension must never run it.
    static func forApp() -> GoCore {
        InstallMarker.apply(defaults: .standard, wipe: KeychainSecretStore.removeAll)
        return shared(sink: nil)
    }
}
