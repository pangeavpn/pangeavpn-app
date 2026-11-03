import XCTest
@testable import PangeaVPN

@MainActor
final class DnsPresetsTests: XCTestCase {
    func testEmptyListIsAutomatic() {
        XCTAssertEqual(DnsPresets.choice(for: []), .automatic)
        XCTAssertEqual(DnsPresets.servers(for: .automatic), [])
    }

    func testCustomHasNoFixedServers() {
        XCTAssertNil(DnsPresets.servers(for: .custom))
    }

    func testPresetAddressesMatchDesktop() {
        XCTAssertEqual(DnsPresets.servers(for: .adguard), ["94.140.14.14", "94.140.15.15"])
        XCTAssertEqual(DnsPresets.servers(for: .cloudflare), ["1.1.1.1", "1.0.0.1"])
        XCTAssertEqual(DnsPresets.servers(for: .quad9), ["9.9.9.9", "149.112.112.112"])
        XCTAssertEqual(DnsPresets.servers(for: .cloudflareFamily), ["1.1.1.3", "1.0.0.3"])
        XCTAssertEqual(DnsPresets.servers(for: .google), ["8.8.8.8", "8.8.4.4"])
    }

    func testEveryPresetRoundTrips() throws {
        for choice in DnsChoice.allCases where choice != .custom {
            let servers = try XCTUnwrap(DnsPresets.servers(for: choice))
            XCTAssertEqual(DnsPresets.choice(for: servers), choice)
        }
    }

    func testReorderedOrPartialPresetIsCustom() {
        XCTAssertEqual(DnsPresets.choice(for: ["1.0.0.1", "1.1.1.1"]), .custom)
        XCTAssertEqual(DnsPresets.choice(for: ["1.1.1.1"]), .custom)
        XCTAssertEqual(DnsPresets.choice(for: ["1.1.1.1", "1.0.0.1", "8.8.8.8"]), .custom)
        XCTAssertEqual(DnsPresets.choice(for: ["9.9.9.10"]), .custom)
    }

    func testBlankInputMeansTheVpnDefault() {
        XCTAssertEqual(DnsPresets.normalize(""), [])
        XCTAssertEqual(DnsPresets.normalize("  , \n"), [])
    }

    func testSplitsOnCommasAndWhitespace() {
        XCTAssertEqual(DnsPresets.normalize("1.1.1.1,1.0.0.1"), ["1.1.1.1", "1.0.0.1"])
        XCTAssertEqual(DnsPresets.normalize(" 1.1.1.1 \n 1.0.0.1, "), ["1.1.1.1", "1.0.0.1"])
    }

    func testDropsDuplicatesAndCanonicalisesLeadingZeros() {
        XCTAssertEqual(DnsPresets.normalize("9.9.9.9, 9.9.9.9"), ["9.9.9.9"])
        XCTAssertEqual(DnsPresets.normalize("001.001.001.001, 1.1.1.1"), ["1.1.1.1"])
    }

    func testRejectsMalformedAddresses() {
        let bad = ["1.1.1", "1.1.1.1.1", "1.1.1.256", "a.b.c.d", "1..1.1", "1111.1.1.1",
                   "+1.1.1.1", "-1.1.1.1", "1.1.1.1/32", "::1", "dns.google", "١.١.١.١"]
        for input in bad {
            XCTAssertNil(DnsPresets.normalize(input), input)
        }
    }

    func testRejectsAddressesNoTunnelResolverCanHave() {
        let blocked = ["0.0.0.0", "10.0.0.1", "100.64.0.1", "100.127.255.255", "169.254.1.1",
                       "172.16.0.1", "172.31.255.255", "192.168.1.1", "224.0.0.1",
                       "255.255.255.255", "127.0.0.1", "127.255.0.1"]
        for input in blocked {
            XCTAssertNil(DnsPresets.normalize(input), input)
        }
    }

    func testAcceptsAddressesJustOutsideTheBlockedRanges() {
        let fine = ["11.0.0.1", "100.63.255.255", "100.128.0.1", "172.15.255.255", "172.32.0.1",
                    "192.167.255.255", "223.255.255.255", "126.255.255.255", "128.0.0.1"]
        for input in fine {
            XCTAssertEqual(DnsPresets.normalize(input), [input], input)
        }
    }

    func testOneBadEntryRejectsTheWholeList() {
        XCTAssertNil(DnsPresets.normalize("1.1.1.1, 192.168.0.1"))
        XCTAssertNil(DnsPresets.normalize("1.1.1.1, nope"))
    }

    func testTypedMapsEasternDigitsAndKeepsPunctuation() {
        XCTAssertEqual(DnsPresets.typed("۱.۱.۱.۱, ١.٠.٠.١"), "1.1.1.1, 1.0.0.1")
        XCTAssertEqual(DnsPresets.typed("8.8.8.8, 8.8.4.4"), "8.8.8.8, 8.8.4.4")
        XCTAssertEqual(DnsPresets.normalize(DnsPresets.typed("۹.۹.۹.۹")), ["9.9.9.9"])
    }

    func testTitlesMatchDesktop() {
        XCTAssertEqual(DnsPresets.title(.automatic), "Automatic (VPN default)")
        XCTAssertEqual(DnsPresets.title(.adguard), "AdGuard DNS - Ads & trackers")
        XCTAssertEqual(DnsPresets.title(.cloudflareFamily), "Cloudflare Family - Malware & adult content")
        XCTAssertEqual(DnsPresets.title(.custom), "Custom")
        XCTAssertEqual(Set(DnsChoice.allCases.map(DnsPresets.title)).count, DnsChoice.allCases.count)
    }

    func testNetworkSummaryShowsOnlyTheMtuByDefault() {
        XCTAssertEqual(SettingsSummary.network(AppSettings()), "MTU 1380")
    }

    func testNetworkSummaryListsEverythingThatIsOn() {
        var settings = AppSettings()
        settings.mtu = 1300
        settings.customDns = ["1.1.1.1", "1.0.0.1"]
        settings.allowLan = true
        settings.postQuantum = true
        let separator = SettingsSummary.separator
        XCTAssertEqual(SettingsSummary.network(settings), ["MTU 1300", "Cloudflare", "Allow LAN", "PQ"].joined(separator: separator))
    }

    func testNetworkSummaryNamesACustomList() {
        var settings = AppSettings()
        settings.customDns = ["9.9.9.10"]
        XCTAssertEqual(SettingsSummary.network(settings), "MTU 1380" + SettingsSummary.separator + "Custom")
    }
}
