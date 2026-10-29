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
}
