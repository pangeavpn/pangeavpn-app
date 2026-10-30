import Foundation
import os

struct DefaultsDomain {
    let defaults: UserDefaults
    let name: String
}

// What a panic wipe clears outside the Go core: the Keychain, preferences and App Group files.
struct PanicWiper {
    let domains: [DefaultsDomain]
    let markerDefaults: UserDefaults
    let container: URL
    let removeKeychain: () -> Bool

    private static let log = Logger(subsystem: "org.pangeavpn", category: "wipe")

    static func live() -> PanicWiper {
        let group = DefaultsDomain(defaults: AppGroup.defaults, name: AppGroup.identifier)
        let standard = Bundle.main.bundleIdentifier.map { DefaultsDomain(defaults: .standard, name: $0) }
        return PanicWiper(
            domains: [group] + (standard.map { [$0] } ?? []),
            markerDefaults: .standard,
            container: AppGroup.containerURL,
            removeKeychain: KeychainSecretStore.removeAll)
    }

    // The install marker returns only once the Keychain is empty, so a failed wipe is retried at launch.
    func run() {
        let keychainCleared = removeKeychain()
        for domain in domains {
            domain.defaults.removePersistentDomain(forName: domain.name)
        }
        if keychainCleared { markerDefaults.set(true, forKey: InstallMarker.key) }
        removeFiles()
    }

    // Preferences go through UserDefaults above; deleting their plist under cfprefsd would not stick.
    private func removeFiles() {
        removeChildren(of: container, keeping: ["Library"])
        removeChildren(of: container.appendingPathComponent("Library", isDirectory: true), keeping: ["Preferences"])
    }

    private func removeChildren(of directory: URL, keeping kept: Set<String>) {
        let files = FileManager.default
        guard files.fileExists(atPath: directory.path) else { return }
        let children: [URL]
        do {
            children = try files.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
        } catch {
            Self.log.error("could not list the app group files (\(error.logCode, privacy: .public))")
            return
        }
        for child in children where !kept.contains(child.lastPathComponent) {
            do {
                try files.removeItem(at: child)
            } catch {
                Self.log.error("could not delete an app group file (\(error.logCode, privacy: .public))")
            }
        }
    }
}
