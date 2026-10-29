import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelControllerTests: XCTestCase {
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
}
