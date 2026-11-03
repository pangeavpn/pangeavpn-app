import XCTest
@testable import PangeaVPN

@MainActor
final class HubStatusTextTests: XCTestCase {
    func testNoActiveMethod() {
        XCTAssertEqual(HubMethodLabels.activeText(.none), "No method in use yet")
        XCTAssertEqual(HubMethodLabels.activeText(HubStatus(active: "", detail: "x")), "No method in use yet")
    }

    func testActiveMethodUsesItsTitle() {
        XCTAssertEqual(HubMethodLabels.activeText(HubStatus(active: "fronted", detail: nil)), "In use: Relay")
        XCTAssertEqual(HubMethodLabels.activeText(HubStatus(active: "reality", detail: "")), "In use: REALITY")
    }

    func testActiveMethodWithDetail() {
        let status = HubStatus(active: "directIp", detail: "203.0.113.7")
        XCTAssertEqual(HubMethodLabels.activeText(status), "In use: Direct IP — 203.0.113.7")
    }

    func testWorks() {
        let result = HubTestResult(method: "directIp", ok: true, ms: 120)
        XCTAssertEqual(HubMethodLabels.testText(result), "Works (120 ms)")
        XCTAssertEqual(HubMethodLabels.tone(result), .ok)
    }

    func testWorksVia() {
        let result = HubTestResult(method: "fronted", ok: true, ms: 87, detail: "relay.example")
        XCTAssertEqual(HubMethodLabels.testText(result), "Works via relay.example (87 ms)")
        XCTAssertEqual(HubMethodLabels.tone(result), .ok)
    }

    func testFailureIgnoresTheDetail() {
        let result = HubTestResult(method: "normal", ok: false, ms: 0, detail: "timeout")
        XCTAssertEqual(HubMethodLabels.testText(result), "Could not reach our servers")
        XCTAssertEqual(HubMethodLabels.tone(result), .failed)
    }

    func testUnavailableReasonsUseDesktopWording() {
        let cases: [(method: String, reason: String, text: String)] = [
            ("directIp", "noAddress", "No address stored yet, and encrypted DNS is off"),
            ("shadowsocks", "noCredentials", "No Shadowsocks credentials stored yet"),
            ("reality", "noCredentials", "No REALITY credentials stored yet"),
            ("fronted", "noRelay", "No relay configured yet"),
            ("normal", "busy", "Another test is still running"),
        ]
        for item in cases {
            let result = HubTestResult(method: item.method, ok: false, ms: 0, unavailable: item.reason)
            XCTAssertEqual(HubMethodLabels.testText(result), item.text, item.reason)
            XCTAssertEqual(HubMethodLabels.tone(result), .unavailable, item.reason)
        }
    }

    func testUnknownReasonFallsBackToTheOutcome() {
        let result = HubTestResult(method: "reality", ok: false, ms: 0, unavailable: "mystery")
        XCTAssertEqual(HubMethodLabels.testText(result), "Could not reach our servers")
        XCTAssertEqual(HubMethodLabels.tone(result), .failed)
    }

    func testOrderMatchesDesktop() {
        XCTAssertEqual(HubMethodLabels.order, ["directIp", "reality", "shadowsocks", "fronted", "normal"])
    }

    func testEnabledFollowsTheOrder() {
        var methods = HubMethods()
        XCTAssertEqual(HubMethodLabels.enabled(in: methods), ["directIp", "reality", "shadowsocks", "fronted"])
        methods.reality = false
        methods.normal = true
        XCTAssertEqual(HubMethodLabels.enabled(in: methods), ["directIp", "shadowsocks", "fronted", "normal"])
        XCTAssertFalse(HubMethodLabels.isEnabled("mystery", in: methods))
    }

    func testProvisioningSummaryListsEnabledTitles() {
        let expected = ["Direct IP", "REALITY", "Shadowsocks", "Relay"].joined(separator: SettingsSummary.separator)
        XCTAssertEqual(SettingsSummary.provisioning(AppSettings()), expected)
    }

    func testProvisioningSummaryIsOffWhenNothingIsEnabled() {
        var settings = AppSettings()
        settings.hubMethods.directIp = false
        settings.hubMethods.reality = false
        settings.hubMethods.shadowsocks = false
        settings.hubMethods.fronted = false
        settings.hubMethods.normal = false
        XCTAssertEqual(SettingsSummary.provisioning(settings), SettingsSummary.off())
    }
}
