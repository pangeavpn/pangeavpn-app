import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelControllerTests: XCTestCase {
    private func profile(auto: Bool, lockdown: Bool) -> TunnelProfile {
        TunnelProfile(strictKillSwitch: false, allowLan: false, autoConnect: auto, lockdown: lockdown)
    }

    func testNEStatusMapsToTunnelState() {
        let cases: [(NEVPNStatus, TunnelState)] = [
            (.invalid, .disconnected),
            (.disconnected, .disconnected),
            (.connecting, .connecting),
            (.reasserting, .connecting),
            (.connected, .connected),
            (.disconnecting, .disconnecting),
        ]
        for (status, expected) in cases {
            XCTAssertEqual(TunnelController.state(for: status), expected, "NEVPNStatus \(status.rawValue)")
        }
    }

    func testOnlyAKillSwitchTunnelThatMayBeStrandedIsDisabled() {
        let cases: [(includeAll: Bool, wasRunning: Bool, stopped: Bool, disable: Bool)] = [
            (true, false, true, true),
            (true, true, false, true),
            (true, true, true, false),
            (false, false, true, false),
            (false, true, false, false),
            (false, true, true, false),
        ]
        for c in cases {
            let got = TunnelController.mustDisable(
                includeAllNetworks: c.includeAll, wasRunning: c.wasRunning, stopped: c.stopped)
            XCTAssertEqual(got, c.disable, "includeAll \(c.includeAll) running \(c.wasRunning) stopped \(c.stopped)")
        }
    }

    func testOnDemandIsWantedForAutoConnectOrLockdown() {
        XCTAssertTrue(OnDemandPolicy.wanted(.standard))
        XCTAssertTrue(OnDemandPolicy.wanted(profile(auto: true, lockdown: false)))
        XCTAssertTrue(OnDemandPolicy.wanted(profile(auto: false, lockdown: true)))
        XCTAssertFalse(OnDemandPolicy.wanted(profile(auto: false, lockdown: false)))
    }

    func testOnDemandIsNeverKeptWhenNotWanted() {
        let off = profile(auto: false, lockdown: false)
        for state in [TunnelState.disconnected, .connecting, .connected, .disconnecting, .error] {
            for enabled in [false, true] {
                XCTAssertFalse(OnDemandPolicy.reconciled(
                    off, state: state, enabled: enabled, awaitingFirstConnect: false), "\(state) \(enabled)")
            }
        }
    }

    func testWantedOnDemandFollowsALiveTunnelOrTheSavedFlag() {
        let cases: [(state: TunnelState, enabled: Bool, awaiting: Bool, on: Bool)] = [
            (.connected, false, false, true),
            (.connected, false, true, true),
            (.connecting, false, false, true),
            (.connecting, false, true, false),
            (.disconnected, false, false, false),
            (.disconnecting, false, false, false),
            (.error, false, false, false),
            (.disconnected, true, false, true),
            (.connecting, true, true, true),
        ]
        for wanted in [profile(auto: true, lockdown: false), profile(auto: false, lockdown: true)] {
            for c in cases {
                let got = OnDemandPolicy.reconciled(
                    wanted, state: c.state, enabled: c.enabled, awaitingFirstConnect: c.awaiting)
                XCTAssertEqual(got, c.on, "\(wanted) \(c.state) enabled \(c.enabled) awaiting \(c.awaiting)")
            }
        }
    }

    func testOnDemandIsOnlyActiveOnAnEnabledConfiguration() {
        XCTAssertTrue(OnDemandPolicy.active(configurationEnabled: true, onDemandEnabled: true))
        XCTAssertFalse(OnDemandPolicy.active(configurationEnabled: false, onDemandEnabled: true))
        XCTAssertFalse(OnDemandPolicy.active(configurationEnabled: true, onDemandEnabled: false))
        XCTAssertFalse(OnDemandPolicy.active(configurationEnabled: false, onDemandEnabled: false))
    }

    func testAutoConnectIsDeclinedOnlyBySwitchingOffUnderTheArmedSession() {
        let auto = profile(auto: true, lockdown: false)
        XCTAssertTrue(OnDemandPolicy.declinedOutside(
            auto, enabled: false, configurationEnabled: true, armedSessionUp: true))
        XCTAssertFalse(OnDemandPolicy.declinedOutside(
            auto, enabled: true, configurationEnabled: true, armedSessionUp: true))
        XCTAssertFalse(OnDemandPolicy.declinedOutside(
            auto, enabled: false, configurationEnabled: false, armedSessionUp: true))
        XCTAssertFalse(OnDemandPolicy.declinedOutside(
            auto, enabled: false, configurationEnabled: true, armedSessionUp: false))
        for other in [profile(auto: true, lockdown: true), profile(auto: false, lockdown: false)] {
            XCTAssertFalse(OnDemandPolicy.declinedOutside(
                other, enabled: false, configurationEnabled: true, armedSessionUp: true), "\(other)")
        }
    }

    func testOnlyARunningSessionTakesMessages() {
        let cases: [(NEVPNStatus, Bool)] = [
            (.invalid, false),
            (.disconnected, false),
            (.connecting, true),
            (.connected, true),
            (.reasserting, true),
            (.disconnecting, false),
        ]
        for (status, expected) in cases {
            XCTAssertEqual(ProviderChannel.canMessage(status), expected, "NEVPNStatus \(status.rawValue)")
        }
    }

    func testSwitchCountsOnlyAnExplicitAcceptance() throws {
        XCTAssertTrue(ProviderChannel.accepted(try JSONEncoder().encode(SwitchReply(accepted: true))))
        XCTAssertFalse(ProviderChannel.accepted(try JSONEncoder().encode(SwitchReply(accepted: false))))
        XCTAssertFalse(ProviderChannel.accepted(nil))
        XCTAssertFalse(ProviderChannel.accepted(Data("not json".utf8)))
    }

    func testSwitchMessageRoundTripsThroughTheProviderParser() throws {
        let request = SwitchRequest(serverId: "fra-1", candidates: ["fra-1", "ams-1"])
        let message = try ProviderMessage.encodeSwitch(request)
        XCTAssertEqual(ProviderMessage.switchRequest(from: message), request)
    }

    func testRequestsCarryTheirMessageKind() {
        XCTAssertEqual(ProviderChannel.stateRequest, Data([ProviderMessage.state.rawValue]))
        XCTAssertEqual(ProviderChannel.logsRequest, Data([ProviderMessage.logs.rawValue]))
    }

    func testLogsReplyDecodesAsUTF8() {
        XCTAssertEqual(ProviderChannel.text(Data("tunnel up · ok".utf8)), "tunnel up · ok")
    }

    func testSwitchErrorsReadAsTunnelErrors() {
        XCTAssertEqual(TunnelSwitchError.notRunning.localizedDescription, TunnelStartError.stopped.localizedDescription)
        XCTAssertEqual(TunnelSwitchError.rejected.localizedDescription, TunnelStartError.connectFailed.localizedDescription)
    }
}
