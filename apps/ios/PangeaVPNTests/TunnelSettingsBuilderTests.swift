import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelSettingsBuilderTests: XCTestCase {
    private let config = TunnelConfig(
        address: "10.66.0.2", prefixLength: 32, dns: ["10.66.0.1", "1.1.1.1"],
        mtu: 1380, serverId: "s", serverName: "n")

    private func keyV4(_ route: NEIPv4Route) -> String {
        "\(route.destinationAddress)/\(route.destinationSubnetMask)"
    }

    private func keyV6(_ route: NEIPv6Route) -> String {
        "\(route.destinationAddress)/\(route.destinationNetworkPrefixLength)"
    }

    func testSettingsDefaultRoutesNoLan() {
        let settings = TunnelSettingsBuilder.make(config, allowLan: false)
        let v4 = settings.ipv4Settings
        let v6 = settings.ipv6Settings
        XCTAssertEqual((v4?.includedRoutes ?? []).map(keyV4), [keyV4(NEIPv4Route.default())])
        XCTAssertTrue((v4?.excludedRoutes ?? []).isEmpty)
        XCTAssertEqual((v6?.includedRoutes ?? []).map(keyV6), [keyV6(NEIPv6Route.default())])
        XCTAssertTrue((v6?.excludedRoutes ?? []).isEmpty)
    }

    func testSettingsAllowLanExcludesPrivate() {
        let settings = TunnelSettingsBuilder.make(config, allowLan: true)
        let v4 = settings.ipv4Settings
        let v6 = settings.ipv6Settings
        let excludedV4 = (v4?.excludedRoutes ?? []).map(keyV4)
        let excludedV6 = (v6?.excludedRoutes ?? []).map(keyV6)
        XCTAssertEqual(excludedV4.count, 4)
        XCTAssertEqual(Set(excludedV4), [
            "10.0.0.0/255.0.0.0", "172.16.0.0/255.240.0.0",
            "192.168.0.0/255.255.0.0", "169.254.0.0/255.255.0.0",
        ])
        XCTAssertEqual(excludedV6.count, 2)
        XCTAssertEqual(Set(excludedV6), ["fc00::/7", "fe80::/10"])
        XCTAssertEqual((v6?.includedRoutes ?? []).map(keyV6), [keyV6(NEIPv6Route.default())])
    }

    func testAllowLanPinsEachResolverInsideTheTunnel() {
        let included = (TunnelSettingsBuilder.make(config, allowLan: true).ipv4Settings?.includedRoutes ?? [])
        XCTAssertEqual(included.map(keyV4), [
            keyV4(NEIPv4Route.default()), "10.66.0.1/255.255.255.255", "1.1.1.1/255.255.255.255",
        ])
    }

    func testDnsDropsLoopbackUnspecifiedMulticastAndNonIPv4() {
        let messy = TunnelConfig(
            address: "10.66.0.2", prefixLength: 32,
            dns: ["127.0.0.1", "0.0.0.0", "224.0.0.251", "2606:4700::1111", "dns.example", " 9.9.9.9 ", "1.1.1.1"],
            mtu: 1380, serverId: "s", serverName: "n")
        XCTAssertEqual(TunnelSettingsBuilder.dnsServers(messy), ["9.9.9.9", "1.1.1.1"])
        XCTAssertEqual(TunnelSettingsBuilder.make(messy, allowLan: false).dnsSettings?.servers, ["9.9.9.9", "1.1.1.1"])
    }

    func testBuildRejectsConfigWithoutUsableDNS() {
        for dns in [[], ["127.0.0.1"], [","]] {
            let bad = TunnelConfig(
                address: "10.66.0.2", prefixLength: 32, dns: dns, mtu: 1380, serverId: "s", serverName: "n")
            XCTAssertThrowsError(try TunnelSettingsBuilder.build(bad, allowLan: false)) {
                XCTAssertEqual($0 as? TunnelStartError, .invalidDNS)
            }
        }
    }

    func testBuildAcceptsUsableConfig() throws {
        let settings = try TunnelSettingsBuilder.build(config, allowLan: false)
        XCTAssertEqual(settings.dnsSettings?.servers, ["10.66.0.1", "1.1.1.1"])
    }

    func testSettingsApplyPassesTheCallbackThroughOnce() {
        let clock = LifecycleClock()
        var results: [Error?] = []
        SettingsApply.run(timeout: 5, scheduler: clock, apply: { done in done(nil) }) { results.append($0) }
        clock.advance(by: 10)
        XCTAssertEqual(results.count, 1)
        XCTAssertNil(results.first ?? nil)
        XCTAssertEqual(clock.pending, [])
    }

    func testSettingsApplyPassesErrorsThrough() {
        let clock = LifecycleClock()
        var results: [Error?] = []
        SettingsApply.run(timeout: 5, scheduler: clock, apply: { done in done(CoreError(message: "x")) }) {
            results.append($0)
        }
        XCTAssertEqual(results.compactMap { $0 as? CoreError }, [CoreError(message: "x")])
    }

    func testSettingsApplyTimesOutAndIgnoresALateCallback() {
        let clock = LifecycleClock()
        var held: ((Error?) -> Void)?
        var results: [Error?] = []
        SettingsApply.run(timeout: 5, scheduler: clock, apply: { held = $0 }) { results.append($0) }
        clock.advance(by: 4.9)
        XCTAssertTrue(results.isEmpty)
        clock.advance(by: 0.2)
        XCTAssertEqual(results.compactMap { $0 as? TunnelStartError }, [.settingsTimeout])
        held?(nil)
        held?(CoreError(message: "late"))
        XCTAssertEqual(results.count, 1)
    }

    func testPrefixToMask() {
        XCTAssertEqual(TunnelSettingsBuilder.mask(prefixLength: 32), "255.255.255.255")
        XCTAssertEqual(TunnelSettingsBuilder.mask(prefixLength: 24), "255.255.255.0")
        XCTAssertEqual(TunnelSettingsBuilder.mask(prefixLength: 12), "255.240.0.0")
        XCTAssertEqual(TunnelSettingsBuilder.mask(prefixLength: 0), "0.0.0.0")
    }

    func testPrefixToMaskClampsOutOfRange() {
        XCTAssertEqual(TunnelSettingsBuilder.mask(prefixLength: 40), "255.255.255.255")
        XCTAssertEqual(TunnelSettingsBuilder.mask(prefixLength: -3), "0.0.0.0")
    }

    func testDnsMatchesAll() {
        let dns = TunnelSettingsBuilder.make(config, allowLan: false).dnsSettings
        XCTAssertEqual(dns?.servers, ["10.66.0.1", "1.1.1.1"])
        XCTAssertEqual(dns?.matchDomains, [""])
    }

    func testAddressesMtuAndRemote() {
        let settings = TunnelSettingsBuilder.make(config, allowLan: false)
        XCTAssertEqual(settings.tunnelRemoteAddress, "127.0.0.1")
        XCTAssertEqual(settings.ipv4Settings?.addresses, ["10.66.0.2"])
        XCTAssertEqual(settings.ipv4Settings?.subnetMasks, ["255.255.255.255"])
        XCTAssertEqual(settings.ipv6Settings?.addresses, ["fd00::1"])
        XCTAssertEqual(settings.ipv6Settings?.networkPrefixLengths, [NSNumber(value: 128)])
        XCTAssertEqual(settings.mtu, NSNumber(value: 1380))
    }

    func testBlockingSettingsRouteEverythingIntoTheTunnel() {
        let settings = TunnelSettingsBuilder.blocking(allowLan: false)
        XCTAssertEqual(settings.ipv4Settings?.addresses, ["192.0.2.2"])
        XCTAssertEqual(settings.ipv4Settings?.subnetMasks, ["255.255.255.255"])
        XCTAssertEqual((settings.ipv4Settings?.includedRoutes ?? []).map(keyV4), [keyV4(NEIPv4Route.default())])
        XCTAssertTrue((settings.ipv4Settings?.excludedRoutes ?? []).isEmpty)
        XCTAssertEqual((settings.ipv6Settings?.includedRoutes ?? []).map(keyV6), [keyV6(NEIPv6Route.default())])
        XCTAssertEqual(settings.dnsSettings?.servers, ["192.0.2.1"])
        XCTAssertEqual(settings.dnsSettings?.matchDomains, [""])
    }

    func testBlockingSettingsKeepTheLanAndItsResolverPin() {
        let v4 = TunnelSettingsBuilder.blocking(allowLan: true).ipv4Settings
        XCTAssertEqual((v4?.excludedRoutes ?? []).count, 4)
        XCTAssertTrue((v4?.includedRoutes ?? []).map(keyV4).contains("192.0.2.1/255.255.255.255"))
    }

    func testExcludedRangesAreCarvedOutAndUnusableOnesSkipped() {
        let ranges = [
            "203.0.113.77/24", "198.51.100.7", "garbage", "10.0.0.0/4", "1.2.3.4/33", "2001:db8::/32", " 192.0.2.0/30 ",
        ]
        let excluded = (TunnelSettingsBuilder.make(config, allowLan: false, excludedRanges: ranges)
            .ipv4Settings?.excludedRoutes ?? []).map(keyV4)
        XCTAssertEqual(excluded, [
            "203.0.113.0/255.255.255.0", "198.51.100.7/255.255.255.255", "192.0.2.0/255.255.255.252",
        ])
    }

    func testResolversStayInsideTheTunnelWhenARangeCoversThem() {
        let included = (TunnelSettingsBuilder.make(config, allowLan: false, excludedRanges: ["1.1.1.0/24"])
            .ipv4Settings?.includedRoutes ?? []).map(keyV4)
        XCTAssertEqual(included, [
            keyV4(NEIPv4Route.default()), "10.66.0.1/255.255.255.255", "1.1.1.1/255.255.255.255",
        ])
    }

    func testExcludedRangesAddToTheLanExclusions() {
        let excluded = TunnelSettingsBuilder.make(config, allowLan: true, excludedRanges: ["203.0.113.0/24"])
            .ipv4Settings?.excludedRoutes ?? []
        XCTAssertEqual(excluded.count, 5)
        XCTAssertEqual(excluded.last.map(keyV4), "203.0.113.0/255.255.255.0")
    }

    // The page's rules decide what the tunnel carves out, so a range it refuses never becomes a route.
    func testExcludedRoutesFollowTheSplitTunnelRules() {
        let ranges = ["10.1.2.3/8", "8.8.8.8", "10.0.0.0/7", "10.0.0.0/", "10.0.0.0/8/1", "", "1.2.3.4/08", "۱۰.۰.۰.۰/۸"]
        let excluded = TunnelSettingsBuilder.excludedRoutes(ranges).map(keyV4)
        XCTAssertEqual(excluded, ["10.0.0.0/255.0.0.0", "8.8.8.8/255.255.255.255", "10.0.0.0/255.0.0.0"])
    }

    func testFingerprintChangesWithEveryAppliedInput() {
        let base = fingerprint(config)
        XCTAssertEqual(base, fingerprint(config))
        XCTAssertNotEqual(base, fingerprint(tunnel(address: "10.66.0.3")))
        XCTAssertNotEqual(base, fingerprint(tunnel(mtu: 1280)))
        XCTAssertNotEqual(base, fingerprint(tunnel(dns: ["9.9.9.9"])))
        XCTAssertNotEqual(base, fingerprint(config, allowLan: true))
        XCTAssertNotEqual(base, fingerprint(config, ranges: ["1.2.3.0/24"]))
    }

    func testFingerprintIgnoresTheServerIdentity() {
        XCTAssertEqual(fingerprint(config), fingerprint(tunnel(serverId: "x")))
    }

    private func fingerprint(
        _ c: TunnelConfig, allowLan: Bool = false, ranges: [String] = []
    ) -> TunnelSettingsFingerprint {
        TunnelSettingsBuilder.fingerprint(c, allowLan: allowLan, excludedRanges: ranges)
    }

    private func tunnel(
        address: String = "10.66.0.2", dns: [String] = ["10.66.0.1", "1.1.1.1"], mtu: Int = 1380, serverId: String = "s"
    ) -> TunnelConfig {
        TunnelConfig(address: address, prefixLength: 32, dns: dns, mtu: mtu, serverId: serverId, serverName: serverId)
    }
}
