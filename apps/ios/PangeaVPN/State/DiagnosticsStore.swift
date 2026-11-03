import Foundation
import Observation
import os

// The build and OS a copied diagnostics summary names.
struct DiagnosticsClient: Equatable, Sendable {
    let appVersion: String
    let osVersion: String

    // Worded as the core's report header, so a copied summary and a sent report agree.
    static var current: DiagnosticsClient {
        let info = ClientInfo.current()
        return DiagnosticsClient(appVersion: info.app, osVersion: info.system)
    }
}

// Desktop words a refused report differently from one that never arrived.
enum ReportFailure: Equatable, Sendable {
    case unreachable, rejected

    static let rejectedMarker = "DIAGNOSTICS_REJECTED"

    init(_ error: Error) {
        self = error.localizedDescription.contains(Self.rejectedMarker) ? .rejected : .unreachable
    }
}

enum DiagnosticsReport {
    static let noteLimit = 500
    static let tailLines = 200

    private static let log = Logger(subsystem: "org.pangeavpn", category: "diagnostics")

    struct Snapshot: Encodable {
        let generatedAtUtc: String
        let timezone: String
        let appVersion: String
        let platform: String
        let osVersion: String
        let status: TunnelStatus
        let settings: SettingsDigest
        let tunnelProfile: ProfileDigest?
        let appLogTail: [String]
        let extensionLogTail: [String]?
    }

    // Named fields only, so a secret later added to AppSettings never reaches the clipboard.
    struct SettingsDigest: Encodable, Equatable {
        let preferredTransport: String
        let customDns: [String]
        let mtu: Int
        let allowLan: Bool
        let autoConnect: Bool
        let deadDrop: Bool
        let postQuantum: Bool
        let lastServerId: String
        let hubMethods: HubMethods

        init(_ settings: AppSettings) {
            preferredTransport = settings.preferredTransport
            customDns = settings.customDns
            mtu = settings.mtu
            allowLan = settings.allowLan
            autoConnect = settings.autoConnect
            deadDrop = settings.deadDrop
            postQuantum = settings.postQuantum
            lastServerId = settings.lastServerId
            hubMethods = settings.hubMethods
        }
    }

    struct ProfileDigest: Encodable, Equatable {
        let strictKillSwitch: Bool
        let allowLan: Bool
        let autoConnect: Bool
        let lockdown: Bool

        init(_ profile: TunnelProfile) {
            strictKillSwitch = profile.strictKillSwitch
            allowLan = profile.allowLan
            autoConnect = profile.autoConnect
            lockdown = profile.lockdown
        }
    }

    static func cappedNote(_ note: String) -> String {
        String(note.trimmingCharacters(in: .whitespacesAndNewlines).prefix(noteLimit))
    }

    // A tunnel that is down has no log to add; the core then reports that section as empty.
    static func extraLogs(_ extensionLog: String?) -> String {
        extensionLog ?? ""
    }

    static func tail(_ text: String, lines limit: Int = tailLines) -> [String] {
        text.split(whereSeparator: \.isNewline).suffix(limit).map(String.init)
    }

    static func snapshot(
        client: DiagnosticsClient, status: TunnelStatus, settings: AppSettings,
        profile: TunnelProfile?, appLog: String, extensionLog: String?, at date: Date
    ) -> Snapshot {
        let stamp = ISO8601DateFormatter()
        stamp.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return Snapshot(
            generatedAtUtc: stamp.string(from: date),
            timezone: TimeZone.current.identifier,
            appVersion: client.appVersion,
            platform: "ios",
            osVersion: client.osVersion,
            status: status,
            settings: SettingsDigest(settings),
            tunnelProfile: profile.map(ProfileDigest.init),
            appLogTail: tail(appLog),
            extensionLogTail: extensionLog.map { tail($0) })
    }

    static func json(_ snapshot: Snapshot) -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys, .withoutEscapingSlashes]
        do {
            let data = try encoder.encode(snapshot)
            return String(decoding: data, as: UTF8.self)
        } catch {
            log.error("diagnostics summary could not be encoded: \(error.logCode, privacy: .public)")
            return "{\"error\":\"diagnostics summary could not be encoded\"}"
        }
    }
}

@MainActor
@Observable
final class DiagnosticsStore {
    // The last logs read, which the copied summary quotes.
    private(set) var appLog = ""
    // nil while the tunnel is down or did not answer.
    private(set) var extensionLog: String?

    private let core: any CoreBridge
    private let tunnel: any TunnelControlling
    private let client: DiagnosticsClient

    private static let log = Logger(subsystem: "org.pangeavpn", category: "diagnostics")

    init(core: any CoreBridge, tunnel: any TunnelControlling, client: DiagnosticsClient = .current) {
        self.core = core
        self.tunnel = tunnel
        self.client = client
    }

    var currentStatus: TunnelStatus { tunnel.status }

    func appLogs() async -> String {
        let core = self.core
        let text = await CoreQueue.perform { core.logs() }
        appLog = text
        return text
    }

    func extensionLogs() async -> String? {
        let text = await tunnel.extensionLogs()
        extensionLog = text
        return text
    }

    // The core redacts both logs and caps their size; the code is what support asks for.
    func send(note: String) async -> Result<String, Error> {
        let capped = DiagnosticsReport.cappedNote(note)
        let tunnelLog = await extensionLogs()
        let extra = DiagnosticsReport.extraLogs(tunnelLog)
        let core = self.core
        do {
            let code = try await CoreQueue.run { try core.sendDiagnostics(note: capped, extraLogs: extra) }
            return .success(code)
        } catch {
            Self.log.error("diagnostics report failed (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            return .failure(error)
        }
    }

    func diagnosticsJSON(status: TunnelStatus, settings: AppSettings, profile: TunnelProfile? = nil) -> String {
        DiagnosticsReport.json(DiagnosticsReport.snapshot(
            client: client, status: status, settings: settings, profile: profile,
            appLog: appLog, extensionLog: extensionLog, at: Date()))
    }
}
