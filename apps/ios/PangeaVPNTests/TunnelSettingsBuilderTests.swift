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
        XCTAssertEqual((v4?.includedRoutes ?? []).map(keyV4), [keyV4(NEIPv4Route.default())])
        XCTAssertEqual((v6?.includedRoutes ?? []).map(keyV6), [keyV6(NEIPv6Route.default())])
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
}
