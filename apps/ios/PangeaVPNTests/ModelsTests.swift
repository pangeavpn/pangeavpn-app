import XCTest
@testable import PangeaVPN

final class ModelsTests: XCTestCase {
    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder().decode(T.self, from: Data(json.utf8))
    }

    func testStatusUnknownStateIsError() throws {
        let status = try decode(TunnelStatus.self, #"{"state":"WEIRD"}"#)
        XCTAssertEqual(status.state, .error)
    }

    func testStatusMissingFieldsDefault() throws {
        let status = try decode(TunnelStatus.self, #"{"state":"CONNECTED"}"#)
        XCTAssertEqual(status.state, .connected)
        XCTAssertEqual(status.bytesIn, 0)
        XCTAssertEqual(status.bytesOut, 0)
        XCTAssertNil(status.activeTransport)
    }

    func testStatusFullDecode() throws {
        let json = #"{"state":"CONNECTING","detail":"d","bytesIn":5,"bytesOut":6,"serverId":"s","serverName":"n","activeTransport":"reality"}"#
        let status = try decode(TunnelStatus.self, json)
        XCTAssertEqual(status.state, .connecting)
        XCTAssertEqual(status.bytesOut, 6)
        XCTAssertEqual(status.activeTransport, "reality")
        XCTAssertEqual(TunnelStatus.disconnected.state, .disconnected)
    }

    func testServerNullLoad() throws {
        let server = try decode(Server.self, #"{"id":"a","name":"A","region":"eu","country":"GB","load":null}"#)
        XCTAssertNil(server.load)
        XCTAssertEqual(server.country, "GB")
    }

    func testSessionDecodesServers() throws {
        let json = #"{"email":"e","name":"n","servers":[{"id":"a","name":"A","region":"eu","country":"GB","load":12}]}"#
        let session = try decode(Session.self, json)
        XCTAssertEqual(session.servers.first?.load, 12)
    }

    func testSubscriptionNullExpiry() throws {
        let sub = try decode(Subscription.self, #"{"status":"active","renews":true,"expiresAt":null}"#)
        XCTAssertNil(sub.expiresAt)
        XCTAssertTrue(sub.renews)
    }

    func testDeviceNullFriendlyName() throws {
        let device = try decode(Device.self, #"{"id":"1","friendlyName":null,"createdAt":"t","status":"active"}"#)
        XCTAssertNil(device.friendlyName)
    }

    func testTunnelConfigDecode() throws {
        let json = #"{"address":"10.0.0.2","prefixLength":32,"dns":["1.1.1.1"],"mtu":1380,"serverId":"s","serverName":"n"}"#
        let config = try decode(TunnelConfig.self, json)
        XCTAssertEqual(config.prefixLength, 32)
        XCTAssertEqual(config.dns, ["1.1.1.1"])
    }

    func testSessionNullServersIsEmpty() throws {
        let session = try decode(Session.self, #"{"email":"e","name":"n","servers":null}"#)
        XCTAssertEqual(session.servers, [])
    }

    func testTunnelConfigNullDnsIsEmpty() throws {
        let json = #"{"address":"10.0.0.2","prefixLength":32,"dns":null,"mtu":1380,"serverId":"s","serverName":"n"}"#
        XCTAssertEqual(try decode(TunnelConfig.self, json).dns, [])
    }

    func testSettingsEmptyObjectUsesGoDefaults() throws {
        let settings = try decode(AppSettings.self, "{}")
        XCTAssertEqual(settings.preferredTransport, "auto")
        XCTAssertEqual(settings.customDns, [])
        XCTAssertEqual(settings.mtu, 1380)
        XCTAssertFalse(settings.allowLan)
        XCTAssertFalse(settings.autoConnect)
        XCTAssertEqual(settings.lastServerId, "")
        XCTAssertTrue(settings.hubMethods.directIp)
        XCTAssertTrue(settings.hubMethods.reality)
        XCTAssertTrue(settings.hubMethods.shadowsocks)
        XCTAssertTrue(settings.hubMethods.fronted)
        XCTAssertFalse(settings.hubMethods.normal)
        XCTAssertEqual(settings.hubMethods.rev, 2)
        XCTAssertTrue(settings.deadDrop)
        XCTAssertEqual(settings, AppSettings())
    }

    func testSettingsPartialHubMethodsKeepDefaults() throws {
        let settings = try decode(AppSettings.self, #"{"hubMethods":{"normal":true}}"#)
        XCTAssertTrue(settings.hubMethods.normal)
        XCTAssertTrue(settings.hubMethods.reality)
    }

    func testSettingsRoundTrip() throws {
        var settings = AppSettings()
        settings.preferredTransport = "anytls"
        settings.customDns = ["9.9.9.9"]
        settings.autoConnect = true
        settings.hubMethods.normal = true
        let data = try JSONEncoder().encode(settings)
        XCTAssertEqual(try JSONDecoder().decode(AppSettings.self, from: data), settings)
    }

    func testUnknownKeysIgnored() throws {
        let status = try decode(TunnelStatus.self, #"{"state":"DISCONNECTED","extra":{"a":1}}"#)
        XCTAssertEqual(status.state, .disconnected)
        let settings = try decode(AppSettings.self, #"{"mtu":1400,"future":true}"#)
        XCTAssertEqual(settings.mtu, 1400)
    }

    func testTransportRawValues() {
        XCTAssertEqual(Transport.allCases.map(\.rawValue),
                       ["auto", "cloak", "reality", "shadowsocks", "hysteria2", "anytls"])
    }
}
