import XCTest
@testable import PangeaVPN

final class FormatTests: XCTestCase {
    func testDuration() {
        let cases: [(Int, String)] = [
            (0, "00:00"), (59, "00:59"), (60, "01:00"), (3599, "59:59"),
            (3600, "1:00:00"), (3661, "1:01:01"), (36000, "10:00:00"), (-5, "00:00"),
        ]
        for (seconds, expected) in cases {
            XCTAssertEqual(Format.duration(seconds: seconds), expected, "\(seconds)")
        }
    }

    func testBytes() {
        let gb15 = Int64(1.5 * 1024 * 1024 * 1024)
        let cases: [(Int64, String)] = [
            (0, "0 B"), (1023, "1023 B"), (1024, "1.0 KB"), (1536, "1.5 KB"),
            (1_048_576, "1.0 MB"), (gb15, "1.5 GB"),
            (1_099_511_627_776, "1.0 TB"), (1_125_899_906_842_624, "1024.0 TB"),
        ]
        for (n, expected) in cases {
            XCTAssertEqual(Format.bytes(n), expected, "\(n)")
        }
    }

    func testSessionClockConnectedWithDate() {
        let now = Date(timeIntervalSince1970: 10_000)
        let start = now.addingTimeInterval(-3661)
        XCTAssertEqual(Format.sessionClock(state: .connected, connectedDate: start, now: now), "1:01:01")
    }

    func testSessionClockConnectedWithoutDate() {
        XCTAssertEqual(Format.sessionClock(state: .connected, connectedDate: nil, now: Date()), Format.emDash)
    }

    func testSessionClockOtherStates() {
        let now = Date()
        let start = now.addingTimeInterval(-60)
        let others: [TunnelState] = [.disconnected, .connecting, .disconnecting, .error]
        for state in others {
            XCTAssertEqual(Format.sessionClock(state: state, connectedDate: start, now: now), Format.emDash)
        }
    }

    func testTransportStatus() {
        XCTAssertEqual(TransportLabels.status("cloak"), "Cloak")
        XCTAssertEqual(TransportLabels.status("reality"), "VLESS+REALITY")
        XCTAssertEqual(TransportLabels.status("shadowsocks"), "Shadowsocks")
        XCTAssertEqual(TransportLabels.status("hysteria2"), "Hysteria2")
        XCTAssertEqual(TransportLabels.status("anytls"), "AnyTLS")
        XCTAssertEqual(TransportLabels.status("snowflake"), "Snowflake")
        XCTAssertEqual(TransportLabels.status("naive"), "NaiveProxy")
        XCTAssertEqual(TransportLabels.status("wireguard"), "WireGuard")
        XCTAssertEqual(TransportLabels.status(nil), "")
        XCTAssertEqual(TransportLabels.status("bogus"), "")
    }

    func testTransportChoice() {
        let expected: [Transport: String] = [
            .auto: "Automatic (recommended)", .cloak: "Cloak", .reality: "VLESS+REALITY",
            .shadowsocks: "Shadowsocks", .hysteria2: "Hysteria2", .anytls: "AnyTLS",
        ]
        XCTAssertEqual(expected.count, Transport.allCases.count)
        for transport in Transport.allCases {
            XCTAssertEqual(TransportLabels.choice(transport), expected[transport])
        }
    }

    func testHubMethodTitles() {
        XCTAssertEqual(HubMethodLabels.title("directIp"), "Direct IP")
        XCTAssertEqual(HubMethodLabels.title("reality"), "REALITY")
        XCTAssertEqual(HubMethodLabels.title("shadowsocks"), "Shadowsocks")
        XCTAssertEqual(HubMethodLabels.title("fronted"), "Relay")
        XCTAssertEqual(HubMethodLabels.title("normal"), "Normal connection")
        XCTAssertEqual(HubMethodLabels.title("mystery"), "mystery")
    }

    func testHubMethodHints() {
        XCTAssertEqual(HubMethodLabels.hint("directIp"), "Connects by IP, no DNS lookup.")
        XCTAssertTrue(HubMethodLabels.hint("reality").hasPrefix("Tunnels account traffic through REALITY"))
        XCTAssertTrue(HubMethodLabels.hint("shadowsocks").hasPrefix("Tunnels account traffic through Shadowsocks"))
        XCTAssertTrue(HubMethodLabels.hint("fronted").hasPrefix("Goes through a relay"))
        XCTAssertTrue(HubMethodLabels.hint("normal").hasPrefix("Plain HTTPS"))
        XCTAssertEqual(HubMethodLabels.hint("mystery"), "")
    }
}
