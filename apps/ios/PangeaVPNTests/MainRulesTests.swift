import XCTest
@testable import PangeaVPN

final class MainRulesTests: XCTestCase {
    private let every: [TunnelState] = [.disconnected, .connecting, .connected, .disconnecting, .error]

    func testDisplayFollowsTheTunnelWhenIdle() {
        for state in every {
            XCTAssertEqual(HeroState.display(state, busy: false, switching: false), state)
        }
    }

    func testSwitchReadsAsConnecting() {
        XCTAssertEqual(HeroState.display(.connected, busy: false, switching: true), .connecting)
    }

    func testStartInFlightReadsAsConnecting() {
        XCTAssertEqual(HeroState.display(.disconnected, busy: true, switching: false), .connecting)
        XCTAssertEqual(HeroState.display(.error, busy: true, switching: false), .connecting)
        XCTAssertEqual(HeroState.display(.disconnecting, busy: true, switching: false), .disconnecting)
    }

    func testReconnectingNeedsAnEarlierTunnel() {
        XCTAssertTrue(HeroState.reconnecting(.connecting, busy: false, sawConnected: true, lastHandshake: 0))
        XCTAssertTrue(HeroState.reconnecting(.connecting, busy: false, sawConnected: false, lastHandshake: 1_700_000_000))
        XCTAssertFalse(HeroState.reconnecting(.connecting, busy: false, sawConnected: false, lastHandshake: 0))
    }

    func testAUserStartIsNotAReconnect() {
        XCTAssertFalse(HeroState.reconnecting(.connecting, busy: true, sawConnected: true, lastHandshake: 5))
        XCTAssertFalse(HeroState.reconnecting(.connected, busy: false, sawConnected: true, lastHandshake: 5))
    }

    func testPausedLineWinsWhileReconnecting() {
        let text = HeroDetailText.resolve(display: .connecting, statusDetail: "retrying", serverName: "London",
                                          error: nil, progress: "Still connecting…", paused: true)
        XCTAssertEqual(text, HeroDetailText.pausedLine)
        XCTAssertEqual(HeroDetailText.pausedLine, "Internet is paused until the VPN is back.")
    }

    func testProgressShowsOnlyWhileConnecting() {
        let connecting = HeroDetailText.resolve(display: .connecting, statusDetail: "", serverName: "London",
                                                error: nil, progress: "Still connecting…", paused: false)
        XCTAssertEqual(connecting, "Still connecting…")
        let connected = HeroDetailText.resolve(display: .connected, statusDetail: "", serverName: "London",
                                               error: nil, progress: "Still connecting…", paused: false)
        XCTAssertEqual(connected, "London")
    }

    func testIdleErrorBeatsTheStatusDetail() {
        let failed = HeroDetailText.resolve(display: .error, statusDetail: "handshake timeout", serverName: "",
                                            error: "Could not connect", progress: nil, paused: false)
        XCTAssertEqual(failed, "Could not connect")
        let up = HeroDetailText.resolve(display: .connected, statusDetail: "tunnel active", serverName: "London",
                                        error: "switch failed", progress: nil, paused: false)
        XCTAssertEqual(up, "tunnel active")
    }

    func testEmptyDetailFallsBackToTheServerName() {
        let text = HeroDetailText.resolve(display: .connected, statusDetail: "", serverName: "London",
                                          error: nil, progress: nil, paused: false)
        XCTAssertEqual(text, "London")
        XCTAssertEqual(HeroDetailText.resolve(display: .disconnected, statusDetail: "", serverName: "",
                                              error: "", progress: nil, paused: false), "")
    }

    func testOnlyIdleStatesCarryTheError() {
        XCTAssertTrue(HeroDetailText.carriesError(.disconnected))
        XCTAssertTrue(HeroDetailText.carriesError(.error))
        XCTAssertFalse(HeroDetailText.carriesError(.connected))
        XCTAssertFalse(HeroDetailText.carriesError(.connecting))
        XCTAssertFalse(HeroDetailText.carriesError(.disconnecting))
    }

    func testPostQuantumChipOnlyWhenConnected() {
        XCTAssertTrue(HeroChips.showsPostQuantum(display: .connected, postQuantum: true))
        XCTAssertFalse(HeroChips.showsPostQuantum(display: .connected, postQuantum: false))
        XCTAssertFalse(HeroChips.showsPostQuantum(display: .connecting, postQuantum: true))
    }

    func testSplitChipNeedsRangesAndNoStrictKillSwitch() {
        let ranges = ["192.168.0.0/16"]
        XCTAssertTrue(HeroChips.showsSplit(display: .connected, enabled: true, ranges: ranges, strictKillSwitch: false))
        XCTAssertFalse(HeroChips.showsSplit(display: .connected, enabled: true, ranges: [], strictKillSwitch: false))
        XCTAssertFalse(HeroChips.showsSplit(display: .connected, enabled: false, ranges: ranges, strictKillSwitch: false))
        XCTAssertFalse(HeroChips.showsSplit(display: .connected, enabled: true, ranges: ranges, strictKillSwitch: true))
        XCTAssertFalse(HeroChips.showsSplit(display: .disconnected, enabled: true, ranges: ranges, strictKillSwitch: false))
    }

    private func action(_ state: TunnelState, busy: Bool = false, switching: Bool = false,
                        reconnecting: Bool = false, lockdown: Bool = false, entitled: Bool = true) -> MainAction {
        MainAction.resolve(state: state, busy: busy, switching: switching, reconnecting: reconnecting,
                           lockdownBlocks: lockdown, entitled: entitled)
    }

    func testIdleOffersConnect() {
        XCTAssertEqual(action(.disconnected), .connect)
        XCTAssertEqual(action(.error), .connect)
        XCTAssertEqual(action(.disconnected, busy: true), .provisioning)
    }

    func testRunningTunnelOffersAWayOut() {
        XCTAssertEqual(action(.connected), .disconnect)
        XCTAssertEqual(action(.connecting), .stop)
        XCTAssertEqual(action(.connected, switching: true), .stop)
        XCTAssertEqual(action(.connecting, reconnecting: true), .disconnect)
        XCTAssertEqual(action(.connecting, busy: true), .provisioning)
        XCTAssertEqual(action(.disconnecting), .disconnecting)
    }

    func testLockdownReplacesDisconnect() {
        XCTAssertEqual(action(.connected, lockdown: true), .lockdown)
        XCTAssertEqual(action(.connecting, reconnecting: true, lockdown: true), .lockdown)
        XCTAssertEqual(action(.error, lockdown: true), .connect)
    }

    func testExpiredHidesConnectButNotTheWayOut() {
        XCTAssertEqual(action(.disconnected, entitled: false), .hidden)
        XCTAssertEqual(action(.connected, entitled: false), .disconnect)
        XCTAssertFalse(MainAction.hidden.isButton)
        XCTAssertFalse(MainAction.lockdown.isButton)
        XCTAssertTrue(MainAction.stop.isButton)
    }
}
