import XCTest
@testable import PangeaVPN

final class SplitTunnelRangesTests: XCTestCase {
    private func ranges(_ text: String, file: StaticString = #filePath, line: UInt = #line) -> [String] {
        switch SplitTunnelRanges.parse(text) {
        case .success(let ranges): return ranges
        case .failure(let error):
            XCTFail("unexpected error \(error)", file: file, line: line)
            return []
        }
    }

    private func error(_ text: String, file: StaticString = #filePath, line: UInt = #line) -> SplitTunnelRangesError? {
        switch SplitTunnelRanges.parse(text) {
        case .success(let ranges):
            XCTFail("unexpectedly accepted \(ranges)", file: file, line: line)
            return nil
        case .failure(let error): return error
        }
    }

    func testBareAddressBecomesSlash32() {
        XCTAssertEqual(ranges("198.51.100.7"), ["198.51.100.7/32"])
    }

    func testRangeKeepsItsPrefix() {
        XCTAssertEqual(ranges("192.0.2.0/24"), ["192.0.2.0/24"])
    }

    func testHostBitsAreCleared() {
        XCTAssertEqual(ranges("10.1.2.3/8"), ["10.0.0.0/8"])
        XCTAssertEqual(ranges("172.16.5.200/20"), ["172.16.0.0/20"])
    }

    func testCommaSpaceAndSemicolonSeparate() {
        XCTAssertEqual(
            ranges("192.0.2.0/24, 198.51.100.7 203.0.113.0/25;10.0.0.0/8"),
            ["192.0.2.0/24", "198.51.100.7/32", "203.0.113.0/25", "10.0.0.0/8"]
        )
    }

    func testNewlinesAndRepeatedSeparatorsAreIgnored() {
        XCTAssertEqual(ranges(" ,\n192.0.2.1 ,, \t198.51.100.2, "), ["192.0.2.1/32", "198.51.100.2/32"])
    }

    func testOtherScriptCommasSeparate() {
        XCTAssertEqual(ranges("192.0.2.1\u{060C}198.51.100.2\u{FF0C}203.0.113.3"),
                       ["192.0.2.1/32", "198.51.100.2/32", "203.0.113.3/32"])
    }

    func testPersianAndArabicDigitsAreRead() {
        XCTAssertEqual(ranges("۱۰.۰.۰.۱"), ["10.0.0.1/32"])
        XCTAssertEqual(ranges("١٩٢.٠.٢.٠/٢٤"), ["192.0.2.0/24"])
    }

    func testEmptyTextClearsTheList() {
        XCTAssertEqual(ranges(""), [])
        XCTAssertEqual(ranges("  , ; \n"), [])
    }

    func testDuplicatesCollapseAfterNormalizing() {
        XCTAssertEqual(ranges("10.0.0.1, 10.0.0.1/32, 10.9.9.9/8, 10.0.0.0/8"), ["10.0.0.1/32", "10.0.0.0/8"])
    }

    func testOrderIsKept() {
        XCTAssertEqual(ranges("203.0.113.9, 192.0.2.9"), ["203.0.113.9/32", "192.0.2.9/32"])
    }

    func testSlash8IsTheWidestAllowed() {
        XCTAssertEqual(ranges("10.0.0.0/8"), ["10.0.0.0/8"])
        XCTAssertEqual(error("10.0.0.0/7")?.prefixTooShort, ["10.0.0.0/7"])
        XCTAssertEqual(error("0.0.0.0/0")?.prefixTooShort, ["0.0.0.0/0"])
    }

    func testMalformedEntriesAreNotIPv4() {
        let bad = [
            "abc", "256.1.1.1", "1.2.3", "1.2.3.4.5", "01.2.3.4", "1.2.3.4/33", "1.2.3.4/",
            "1.2.3.4/08", "::1", "2001:db8::/32", "1.2.3.4/24/8", "1..3.4", "+1.2.3.4", "1.2.3.4/x",
        ]
        for token in bad {
            XCTAssertEqual(SplitTunnelRanges.normalize(token), .notIPv4, token)
        }
    }

    func testZeroOctetsAreAllowed() {
        XCTAssertEqual(ranges("0.0.0.0/8"), ["0.0.0.0/8"])
    }

    func testErrorsNameEachBadEntryOnceInOrder() {
        let result = error("x, 10.0.0.0/4, 192.0.2.1, x, 300.1.1.1, 10.0.0.0/4")
        XCTAssertEqual(result, SplitTunnelRangesError(notIPv4: ["x", "300.1.1.1"], prefixTooShort: ["10.0.0.0/4"], tooMany: false))
    }

    func testErrorsKeepTheTokenAsTyped() {
        XCTAssertEqual(error("۱.۲.۳")?.notIPv4, ["۱.۲.۳"])
    }

    func testThirtyTwoRangesFit() {
        XCTAssertEqual(ranges(addresses(32)).count, SplitTunnelRanges.maxEntries)
    }

    func testThirtyThreeRangesAreTooMany() {
        XCTAssertEqual(error(addresses(33)), SplitTunnelRangesError(notIPv4: [], prefixTooShort: [], tooMany: true))
    }

    func testDuplicatesDoNotCountTowardTheLimit() {
        XCTAssertEqual(ranges(addresses(32) + ", 10.0.0.1").count, 32)
    }

    func testTooManyIsReportedAlongsideBadEntries() {
        let result = error(addresses(33) + ", nope")
        XCTAssertEqual(result?.notIPv4, ["nope"])
        XCTAssertEqual(result?.tooMany, true)
    }

    func testTextJoinsWithCommas() {
        XCTAssertEqual(SplitTunnelRanges.text(["192.0.2.0/24", "198.51.100.7/32"]), "192.0.2.0/24, 198.51.100.7/32")
        XCTAssertEqual(SplitTunnelRanges.text([]), "")
    }

    func testSavedTextParsesBackToTheSameList() {
        let saved = ranges("10.1.2.3/8, 192.0.2.7")
        XCTAssertEqual(ranges(SplitTunnelRanges.text(saved)), saved)
    }

    private func addresses(_ count: Int) -> String {
        (1...count).map { "10.0.0.\($0)" }.joined(separator: ", ")
    }
}

@MainActor
final class ProtectionSummaryTests: XCTestCase {
    private var defaults: UserDefaults!
    private var suite = ""

    override func setUpWithError() throws {
        (defaults, suite) = try makeTestDefaults()
    }

    override func tearDown() {
        defaults.removePersistentDomain(forName: suite)
        AccentTheme.shared.accent = .terra
    }

    private func store() -> SettingsStore {
        SettingsStore(core: FakeCore(), defaults: defaults)
    }

    func testSplitTunnelSummary() {
        let settings = store()
        settings.setSplitTunnelRanges(["10.0.0.0/8"])
        XCTAssertEqual(SettingsSummary.splitTunnel(settings), SettingsSummary.off())
        settings.setSplitTunnelEnabled(true)
        XCTAssertEqual(SettingsSummary.splitTunnel(settings), "1 range")
        settings.setSplitTunnelRanges(["10.0.0.0/8", "192.0.2.0/24"])
        XCTAssertEqual(SettingsSummary.splitTunnel(settings), "2 ranges")
        settings.setSplitTunnelRanges([])
        XCTAssertEqual(SettingsSummary.splitTunnel(settings), "Nothing excluded")
    }

    func testKillSwitchSummary() {
        let settings = store()
        XCTAssertEqual(SettingsSummary.killSwitch(settings), "Always on")
        settings.setKillSwitch(true)
        XCTAssertEqual(SettingsSummary.killSwitch(settings), "Strict")
    }

    func testStartupSummary() {
        let settings = store()
        XCTAssertEqual(SettingsSummary.startup(settings), "Auto-connect")
        settings.setLockdown(true)
        XCTAssertEqual(SettingsSummary.startup(settings), "Auto-connect" + SettingsSummary.separator + "Lockdown")
        settings.setAutoConnect(false)
        settings.setLockdown(false)
        XCTAssertEqual(SettingsSummary.startup(settings), SettingsSummary.off())
    }

    func testNotificationsSummary() {
        let settings = store()
        XCTAssertEqual(SettingsSummary.notifications(settings), "Connection alerts")
        settings.setConnectionAlerts(false)
        XCTAssertEqual(SettingsSummary.notifications(settings), SettingsSummary.off())
    }
}
