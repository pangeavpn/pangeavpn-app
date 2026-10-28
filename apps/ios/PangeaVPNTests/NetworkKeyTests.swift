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
    }
}
