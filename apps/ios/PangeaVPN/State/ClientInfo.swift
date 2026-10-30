import Foundation

// What the core cannot learn itself and puts in diagnostics reports.
enum ClientInfo {
    static func appVersion(short: String?, build: String?) -> String {
        let version = nonEmpty(short) ?? "unknown"
        guard let build = nonEmpty(build), build != version else { return version }
        return "\(version) (\(build))"
    }

    static func osVersion(_ version: OperatingSystemVersion) -> String {
        "\(version.majorVersion).\(version.minorVersion).\(version.patchVersion)"
    }

    static func current(bundle: Bundle = .main) -> (app: String, system: String) {
        let app = appVersion(
            short: bundle.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String,
            build: bundle.object(forInfoDictionaryKey: "CFBundleVersion") as? String)
        return (app, osVersion(ProcessInfo.processInfo.operatingSystemVersion))
    }

    private static func nonEmpty(_ value: String?) -> String? {
        guard let value, !value.isEmpty else { return nil }
        return value
    }
}
