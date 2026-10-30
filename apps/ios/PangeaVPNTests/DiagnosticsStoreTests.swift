import XCTest
@testable import PangeaVPN

private let testClient = DiagnosticsClient(appVersion: "1.2.3 (45)", osVersion: "17.5.1")

@MainActor
private func makeStore(core: FakeCore? = nil, tunnel: FakeTunnel? = nil) -> DiagnosticsStore {
    DiagnosticsStore(core: core ?? FakeCore(), tunnel: tunnel ?? FakeTunnel(), client: testClient)
}

private func decode(_ json: String) throws -> [String: Any] {
    let object = try JSONSerialization.jsonObject(with: Data(json.utf8))
    return try XCTUnwrap(object as? [String: Any])
}

@MainActor
final class DiagnosticsStoreTests: XCTestCase {
    func testSendReturnsTheReportCode() async throws {
        let core = FakeCore()
        core.diagnosticsResult = .success("7KQ2-M9XD")
        let result = await makeStore(core: core).send(note: "drops after sleep")
        XCTAssertEqual(try result.get(), "7KQ2-M9XD")
        XCTAssertEqual(core.calls, ["sendDiagnostics(drops after sleep)"])
    }

    func testSendCapsTheNoteAt500Characters() async {
        let core = FakeCore()
        _ = await makeStore(core: core).send(note: String(repeating: "a", count: 620))
        XCTAssertEqual(core.calls.last, "sendDiagnostics(\(String(repeating: "a", count: 500)))")
    }

    func testSendTrimsTheNote() async {
        let core = FakeCore()
        _ = await makeStore(core: core).send(note: "  \n slow \n ")
        XCTAssertEqual(core.calls.last, "sendDiagnostics(slow)")
    }

    func testSendFetchesTheTunnelLogFirst() async {
        let tunnel = FakeTunnel()
        tunnel.logsValue = "extension up"
        let store = makeStore(tunnel: tunnel)
        _ = await store.send(note: "")
        XCTAssertEqual(store.extensionLog, "extension up")
    }

    func testSendReturnsTheFailure() async {
        let core = FakeCore()
        core.diagnosticsResult = .failure(CoreError(message: "DIAGNOSTICS_UNREACHABLE: dial tcp: timeout"))
        let result = await makeStore(core: core).send(note: "")
        guard case .failure(let error) = result else { return XCTFail("expected a failure") }
        XCTAssertEqual(ReportFailure(error), .unreachable)
    }

    func testRejectedReportsAreToldApart() {
        let rejected = CoreError(message: "DIAGNOSTICS_REJECTED: hub answered 429 (RATE_LIMITED)")
        XCTAssertEqual(ReportFailure(rejected), .rejected)
        XCTAssertEqual(ReportFailure(CoreError(message: "DIAGNOSTICS_UNREACHABLE: offline")), .unreachable)
        XCTAssertEqual(ReportFailure(CoreError(message: "something else")), .unreachable)
    }

    func testExtraLogsAreEmptyWhenTheTunnelIsDown() {
        XCTAssertEqual(DiagnosticsReport.extraLogs(nil), "")
        XCTAssertEqual(DiagnosticsReport.extraLogs("line"), "line")
    }

    func testAppLogsReadTheCoreAndKeepTheText() async {
        let core = FakeCore()
        core.logsValue = "one\ntwo"
        let store = makeStore(core: core)
        let text = await store.appLogs()
        XCTAssertEqual(text, "one\ntwo")
        XCTAssertEqual(store.appLog, "one\ntwo")
    }

    func testExtensionLogsAreNilWhenTheTunnelDoesNotAnswer() async {
        let store = makeStore()
        let text = await store.extensionLogs()
        XCTAssertNil(text)
        XCTAssertNil(store.extensionLog)
    }

    func testTailKeepsTheNewestLines() {
        XCTAssertEqual(DiagnosticsReport.tail("1\n2\r\n3\n", lines: 2), ["2", "3"])
        XCTAssertEqual(DiagnosticsReport.tail("", lines: 5), [])
    }

    func testDiagnosticsJSONNamesTheBuildStatusAndSettings() async throws {
        let core = FakeCore()
        core.logsValue = "app started"
        let tunnel = FakeTunnel()
        tunnel.logsValue = "tunnel up"
        let store = makeStore(core: core, tunnel: tunnel)
        _ = await store.appLogs()
        _ = await store.extensionLogs()
        var settings = AppSettings()
        settings.customDns = ["9.9.9.9"]
        let status = TunnelStatus(state: .connected, detail: "", bytesIn: 10, bytesOut: 20,
                                  serverId: "uk-lon-1", serverName: "London", activeTransport: "reality")
        let json = try decode(store.diagnosticsJSON(status: status, settings: settings, profile: .standard))
        XCTAssertEqual(json["appVersion"] as? String, "1.2.3 (45)")
        XCTAssertEqual(json["osVersion"] as? String, "17.5.1")
        XCTAssertEqual(json["platform"] as? String, "ios")
        XCTAssertNotNil(json["generatedAtUtc"] as? String)
        let state = try XCTUnwrap(json["status"] as? [String: Any])
        XCTAssertEqual(state["state"] as? String, "CONNECTED")
        XCTAssertEqual(state["serverId"] as? String, "uk-lon-1")
        let summary = try XCTUnwrap(json["settings"] as? [String: Any])
        XCTAssertEqual(summary["customDns"] as? [String], ["9.9.9.9"])
        let profile = try XCTUnwrap(json["tunnelProfile"] as? [String: Any])
        XCTAssertEqual(profile["autoConnect"] as? Bool, true)
        XCTAssertEqual(json["appLogTail"] as? [String], ["app started"])
        XCTAssertEqual(json["extensionLogTail"] as? [String], ["tunnel up"])
    }

    func testDiagnosticsJSONCarriesOnlyNamedSettingsAndNoAccountNumber() throws {
        let core = FakeCore()
        core.accountNumberValue = "ABCDEFGHJKMNPQRSTVWXYZ01"
        let store = makeStore(core: core)
        let text = store.diagnosticsJSON(status: .disconnected, settings: AppSettings())
        let summary = try XCTUnwrap(try decode(text)["settings"] as? [String: Any])
        let expected: Set<String> = [
            "preferredTransport", "customDns", "mtu", "allowLan", "autoConnect",
            "deadDrop", "postQuantum", "lastServerId", "hubMethods",
        ]
        XCTAssertEqual(Set(summary.keys), expected)
        XCTAssertFalse(text.contains(core.accountNumberValue))
        XCTAssertFalse(core.calls.contains("accountNumber"))
    }

    func testDiagnosticsJSONKeepsOnlyTheLastLogLines() async throws {
        let core = FakeCore()
        core.logsValue = (1...250).map { "line \($0)" }.joined(separator: "\n")
        let store = makeStore(core: core)
        _ = await store.appLogs()
        let json = try decode(store.diagnosticsJSON(status: .disconnected, settings: AppSettings()))
        let tail = try XCTUnwrap(json["appLogTail"] as? [String])
        XCTAssertEqual(tail.count, DiagnosticsReport.tailLines)
        XCTAssertEqual(tail.last, "line 250")
        XCTAssertNil(json["extensionLogTail"])
        XCTAssertNil(json["tunnelProfile"])
    }

    func testDeveloperSummaryFollowsTheSetting() throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let settings = SettingsStore(core: FakeCore(), defaults: defaults)
        XCTAssertEqual(SettingsSummary.developer(settings), SettingsSummary.off())
        settings.setDeveloperMode(true)
        XCTAssertEqual(SettingsSummary.developer(settings), String(localized: "developer_summary_on", defaultValue: "On"))
    }
}
