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

    // Verbatim json.Marshal of a Go serverInfo, transport blocks included.
    func testServerDecodesAFullGoServer() throws {
        let json = #"{"id":"eu-1","name":"Frankfurt","region":"eu","country":"DE","load":7,"cloak":{"remoteHost":"h","uid":"u","publicKey":"k"},"naive":{"remoteHost":"h","remotePort":443,"username":"u","password":"p"},"reality":{"remoteHost":"h","remotePort":443,"uuid":"x","publicKey":"k","shortId":"s"},"controlPlaneReality":{"remoteHost":"h","remotePort":8443,"uuid":"x","publicKey":"k","shortId":"s","serverName":"n"},"frontedEndpoints":["a.example"]}"#
        let server = try decode(Server.self, json)
        XCTAssertEqual(server, Server(id: "eu-1", name: "Frankfurt", region: "eu", country: "DE", load: 7,
                                      transports: ["cloak", "wireguard", "naive", "reality"]))
    }

    // Only a top-level block counts: control-plane credentials don't carry user traffic.
    func testServerTransportsComeFromTopLevelBlocks() throws {
        let json = #"{"id":"a","name":"A","region":"eu","country":"GB","load":1,"hysteria2":{"remoteHost":"h"},"anytls":null,"controlPlaneShadowsocks":{"server":"s"}}"#
        let server = try decode(Server.self, json)
        XCTAssertEqual(server.transports, ["cloak", "wireguard", "hysteria2"])
        XCTAssertTrue(server.supports("auto"))
        XCTAssertTrue(server.supports("cloak"))
        XCTAssertTrue(server.supports("hysteria2"))
        XCTAssertFalse(server.supports("anytls"))
        XCTAssertFalse(server.supports("shadowsocks"))
    }

    func testDeviceCurrentDefaultsToFalse() throws {
        let plain = try decode(Device.self, #"{"id":"1","friendlyName":"A","createdAt":"t","status":"active"}"#)
        XCTAssertFalse(plain.current)
        let mine = try decode(Device.self, #"{"id":"1","friendlyName":"A","createdAt":"t","status":"active","current":true}"#)
        XCTAssertTrue(mine.current)
    }

    func testSubscriptionEntitlement() throws {
        XCTAssertNil(try decode(Subscription.self, #"{"status":"active","renews":true,"expiresAt":null}"#).entitled)
        let expired = try decode(Subscription.self, #"{"status":"expired","renews":false,"expiresAt":"2026-01-01","entitled":false}"#)
        XCTAssertEqual(expired.entitled, false)
    }

    func testStatusLivenessFields() throws {
        let status = try decode(TunnelStatus.self, #"{"state":"CONNECTED","lastHandshake":1793312522,"postQuantum":true}"#)
        XCTAssertEqual(status.lastHandshake, 1_793_312_522)
        XCTAssertTrue(status.postQuantum)
        XCTAssertEqual(TunnelStatus.disconnected.lastHandshake, 0)
        XCTAssertFalse(TunnelStatus.disconnected.postQuantum)
    }

    func testHubTestResultDefaults() throws {
        XCTAssertEqual(try decode(HubTestResult.self, #"{"ok":true,"ms":42}"#), HubTestResult(ok: true, ms: 42))
        let busy = try decode(HubTestResult.self, #"{"method":"reality","ok":false,"ms":0,"unavailable":"busy"}"#)
        XCTAssertEqual(busy, HubTestResult(method: "reality", ok: false, ms: 0, unavailable: "busy"))
    }

    func testHubStatusNullActive() throws {
        let idle = try decode(HubStatus.self, #"{"methods":{"directIp":true},"active":null,"detail":null}"#)
        XCTAssertEqual(idle, .none)
        let live = try decode(HubStatus.self, #"{"active":"reality","detail":"node 2"}"#)
        XCTAssertEqual(live.active, "reality")
    }

    // The core's public shape: a flag per transport and no credentials.
    func testServerDecodesThePublicShape() throws {
        let json = #"{"id":"a","name":"A","region":"eu","country":"GB","load":3,"multihop":true,"cloak":true,"reality":true,"naive":false,"hysteria2":false,"shadowsocks":true,"anytls":false,"snowflake":false}"#
        let server = try decode(Server.self, json)
        XCTAssertEqual(server.transports, ["cloak", "wireguard", "reality", "shadowsocks"])
        XCTAssertTrue(server.multihop)
        XCTAssertEqual(try decode(Server.self, String(decoding: JSONEncoder().encode(server), as: UTF8.self)), server)
    }

    func testStatusEntryFields() throws {
        let status = try decode(TunnelStatus.self, #"{"state":"CONNECTED","serverId":"x","entryServerId":"e","entryServerName":"E"}"#)
        XCTAssertEqual(status.entryServerId, "e")
        XCTAssertEqual(status.entryServerName, "E")
        XCTAssertEqual(TunnelStatus.disconnected.entryServerId, "")
    }

    func testSubscriptionCachedFlag() throws {
        XCTAssertTrue(try decode(Subscription.self, #"{"status":"active","renews":true,"expiresAt":null,"cached":true}"#).cached)
        XCTAssertFalse(try decode(Subscription.self, #"{"status":"active","renews":true,"expiresAt":null}"#).cached)
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
        XCTAssertFalse(settings.postQuantum)
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
