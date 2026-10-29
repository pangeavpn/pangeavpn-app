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

    private func status(_ state: TunnelState, detail: String) -> TunnelStatus {
        TunnelStatus(state: state, detail: detail, bytesIn: 0, bytesOut: 0,
                     serverId: "s", serverName: "n", activeTransport: nil)
    }

    func testReconnectDetailFillsInWhileGoIsIdle() {
        for state in [TunnelState.disconnected, .error, .disconnecting] {
            let shown = ProviderMessage.status(status(state, detail: "raw"), reconnectDetail: "why")
            XCTAssertEqual(shown.detail, "why", "\(state)")
            XCTAssertEqual(shown.state, state)
        }
    }

    func testLiveCoreDetailWinsOverReconnectDetail() {
        for state in [TunnelState.connected, .connecting] {
            XCTAssertEqual(ProviderMessage.status(status(state, detail: "starting reality"), reconnectDetail: "why").detail,
                           "starting reality")
        }
    }

    func testCoreErrorDetailIsMappedForTheUser() {
        let failed = status(.error, detail: "no transport could establish a tunnel: cloak: dial 203.0.113.9")
        XCTAssertEqual(ProviderMessage.status(failed, reconnectDetail: nil).detail,
                       TunnelStartError.connectFailed.errorDescription)
        let expired = status(.error, detail: "SUBSCRIPTION_EXPIRED: this account's subscription has expired")
        XCTAssertEqual(ProviderMessage.status(expired, reconnectDetail: nil).detail,
                       TunnelStartError.subscriptionExpired.errorDescription)
    }

    func testStateReplyCarriesTheReconnectDetail() throws {
        let core = FakeCore()
        core.currentState = status(.disconnected, detail: "")
        let reply = try XCTUnwrap(ProviderMessage.reply(to: Data([1]), core: core, reconnectDetail: "why"))
        XCTAssertEqual(try JSONDecoder().decode(TunnelStatus.self, from: reply).detail, "why")
    }

    func testUnknownOrEmptyMessageHasNoReply() throws {
        let core = FakeCore()
        XCTAssertNil(try ProviderMessage.reply(to: Data(), core: core))
        XCTAssertNil(try ProviderMessage.reply(to: Data([0]), core: core))
        XCTAssertNil(try ProviderMessage.reply(to: Data([0xFF]), core: core))
    }
}
