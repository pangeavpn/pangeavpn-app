import XCTest
@testable import PangeaVPN

final class ProviderMessageTests: XCTestCase {
    func testStateRawValue() {
        XCTAssertEqual(ProviderMessage.state.rawValue, 1)
    }

    func testStateReplyEncodesCoreStatus() throws {
        let core = FakeCore()
        core.currentState = TunnelStatus(
            state: .connected, detail: "", bytesIn: 5, bytesOut: 6,
            serverId: "s", serverName: "n", activeTransport: "reality")
        let reply = try XCTUnwrap(ProviderMessage.reply(to: Data([1]), core: core))
        XCTAssertEqual(try JSONDecoder().decode(TunnelStatus.self, from: reply), core.currentState)
    }

    func testUnknownOrEmptyMessageHasNoReply() throws {
        let core = FakeCore()
        XCTAssertNil(try ProviderMessage.reply(to: Data(), core: core))
        XCTAssertNil(try ProviderMessage.reply(to: Data([0]), core: core))
        XCTAssertNil(try ProviderMessage.reply(to: Data([0xFF]), core: core))
    }
}
