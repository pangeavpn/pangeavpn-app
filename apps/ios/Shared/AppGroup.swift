import Foundation
import os

enum AppGroup {
    static let identifier = "group.org.pangeavpn.app"

    // Resolved once per process, so a missing group is logged once rather than on every use.
    static let containerURL: URL = resolveContainer()

    static var defaults: UserDefaults {
        UserDefaults(suiteName: identifier) ?? .standard
    }

    private static func resolveContainer() -> URL {
        if let url = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: identifier) {
            return url
        }
        // Signed builds only land here with a misconfigured group, which splits app and extension state.
        Logger(subsystem: "org.pangeavpn", category: "appgroup")
            .error("app group container unavailable, falling back to a private directory")
        let fallback = try? FileManager.default.url(
            for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
        return fallback ?? FileManager.default.temporaryDirectory
    }
}
