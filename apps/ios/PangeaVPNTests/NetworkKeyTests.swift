import Network
import XCTest
@testable import PangeaVPN

final class NetworkKeyTests: XCTestCase {
    private func v4(_ name: String, _ address: String) -> InterfaceAddress {
        InterfaceAddress(name: name, address: address, isIPv6: false)
    }

    private func v6(_ name: String, _ address: String) -> InterfaceAddress {
        InterfaceAddress(name: name, address: address, isIPv6: true)
    }

    func testEmptyInputGivesEmptyKey() {
        XCTAssertEqual(NetworkKey.make([]), "")
    }

    func testIPv4TokenIsInterfaceAndAddress() {
        XCTAssertEqual(NetworkKey.make([v4("en0", "192.168.1.20")]), "en0:192.168.1.20")
    }

    func testIPv6TokenKeepsOnlyTheSlash64() {
        XCTAssertEqual(
            NetworkKey.make([v6("en0", "2001:db8:1:2:aaaa:bbbb:cccc:dddd")]),
            "en0:2001:db8:1:2:0:0:0:0/64"
        )
    }

    func testIPv6HostBitsRotationKeepsKeyStable() {
        let first = NetworkKey.make([v6("en0", "2001:db8:1:2:1:2:3:4")])
        let second = NetworkKey.make([v6("en0", "2001:db8:1:2:ffff:eeee:dddd:cccc")])
        XCTAssertEqual(first, second)
    }

    func testTunnelAndLocalInterfacesAreSkipped() {
        let addrs = [
            v4("utun3", "10.0.0.2"), v4("lo0", "127.0.0.1"), v4("ipsec0", "10.1.1.1"),
            v4("awdl0", "10.2.2.2"), v4("llw0", "10.3.3.3"), v4("en0", "192.168.1.20"),
        ]
        XCTAssertEqual(NetworkKey.make(addrs), "en0:192.168.1.20")
    }

    func testLinkLocalAndLoopbackAddressesAreSkipped() {
        let addrs = [
            v4("en0", "169.254.10.10"), v6("en0", "fe80::1"), v6("en0", "::1"),
            v6("en0", "ff02::1"), v4("en0", "224.0.0.251"), v4("en0", "0.0.0.0"),
        ]
        XCTAssertEqual(NetworkKey.make(addrs), "")
    }

    func testTokensAreSortedAndJoinedWithPipe() {
        let addrs = [v4("en1", "10.0.0.5"), v4("en0", "192.168.1.20"), v6("en0", "2001:db8::1")]
        XCTAssertEqual(
            NetworkKey.make(addrs),
            "en0:192.168.1.20|en0:2001:db8:0:0:0:0:0:0/64|en1:10.0.0.5"
        )
    }

    func testCurrentDoesNotCrash() {
        _ = NetworkKey.current()
        _ = NetworkKey.current(restrictedTo: [path("en0", .wifi)])
    }

    func testIPv6ScopeSuffixIsStripped() {
        XCTAssertEqual(NetworkKey.make([v6("en0", "2001:db8:1:2::5%en0")]), "en0:2001:db8:1:2:0:0:0:0/64")
        XCTAssertEqual(NetworkKey.make([v6("en0", "fe80::1%en0")]), "")
    }

    func testUnspecifiedAddressesAreSkipped() {
        XCTAssertEqual(NetworkKey.make([v6("en0", "::"), v4("en0", "0.0.0.0")]), "")
    }

    private func path(_ name: String, _ type: NWInterface.InterfaceType) -> PathInterface {
        PathInterface(name: name, type: type)
    }

    private let wifiAndCell = [
        InterfaceAddress(name: "en0", address: "192.168.1.20", isIPv6: false),
        InterfaceAddress(name: "pdp_ip0", address: "10.20.30.40", isIPv6: false),
    ]

    func testRestrictedKeyListsPathInterfacesInOrderThenTheirAddresses() {
        XCTAssertEqual(
            NetworkKey.make(wifiAndCell, restrictedTo: [path("en0", .wifi), path("pdp_ip0", .cellular)]),
            "wifi=en0,cell=pdp_ip0|en0:192.168.1.20|pdp_ip0:10.20.30.40")
    }

    func testWifiDropChangesTheKeyEvenWhileItsAddressLingers() {
        let both = NetworkKey.make(wifiAndCell, restrictedTo: [path("en0", .wifi), path("pdp_ip0", .cellular)])
        let cellOnly = NetworkKey.make(wifiAndCell, restrictedTo: [path("pdp_ip0", .cellular)])
        XCTAssertEqual(cellOnly, "cell=pdp_ip0|pdp_ip0:10.20.30.40")
        XCTAssertNotEqual(both, cellOnly)
    }

    func testSecondaryInterfacesOffThePathDoNotChangeTheKey() {
        let quiet = NetworkKey.make(wifiAndCell, restrictedTo: [path("en0", .wifi)])
        let noisy = NetworkKey.make(
            wifiAndCell + [v4("pdp_ip1", "10.9.9.9"), v4("bridge100", "172.20.10.1"), v4("en2", "169.254.3.3")],
            restrictedTo: [path("en0", .wifi)])
        XCTAssertEqual(quiet, "wifi=en0|en0:192.168.1.20")
        XCTAssertEqual(noisy, quiet)
    }

    func testPreferenceOrderIsPartOfTheKey() {
        let wifiFirst = NetworkKey.make(wifiAndCell, restrictedTo: [path("en0", .wifi), path("pdp_ip0", .cellular)])
        let cellFirst = NetworkKey.make(wifiAndCell, restrictedTo: [path("pdp_ip0", .cellular), path("en0", .wifi)])
        XCTAssertNotEqual(wifiFirst, cellFirst)
    }

    func testRestrictedKeyDropsVirtualInterfaces() {
        let addrs = [v4("utun3", "10.0.0.2"), v4("lo0", "127.0.0.1")]
        XCTAssertEqual(NetworkKey.make(addrs, restrictedTo: [path("utun3", .other), path("lo0", .loopback)]), "")
        XCTAssertEqual(NetworkKey.make(addrs, restrictedTo: [path("ipsec0", .cellular)]), "")
        XCTAssertEqual(NetworkKey.make([], restrictedTo: []), "")
    }

    func testRestrictedKeyWithoutAddressesStillNamesTheInterfaces() {
        XCTAssertEqual(NetworkKey.make([], restrictedTo: [path("en0", .wifi)]), "wifi=en0")
        XCTAssertEqual(NetworkKey.make([], restrictedTo: [path("en5", .wiredEthernet)]), "wired=en5")
    }
}
