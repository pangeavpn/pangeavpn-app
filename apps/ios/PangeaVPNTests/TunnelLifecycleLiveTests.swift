import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelLifecycleLiveTests: XCTestCase {
    func testNewSplitRangesApplyWithoutRestartingTheCore() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.useSplitTunnel(["203.0.113.0/24"])
        rig.lifecycle.applyLive()
        XCTAssertEqual(rig.core.calls, ["reload", "liveConfig"])
        XCTAssertEqual(rig.host.applyCount, 1)
        let excluded = rig.host.lastSettings?.ipv4Settings?.excludedRoutes ?? []
        XCTAssertEqual(excluded.map(\.destinationAddress), ["203.0.113.0"])
        XCTAssertEqual(rig.events, [])
    }

    func testNewDNSAndMTUApplyToTheRunningInterface() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.core.liveTunnel = LifecycleCoreFake.tunnelConfig(dns: ["9.9.9.9"], mtu: 1280)
        rig.lifecycle.applyLive()
        XCTAssertEqual(rig.host.applyCount, 1)
        XCTAssertEqual(rig.host.lastSettings?.mtu, NSNumber(value: 1280))
        XCTAssertEqual(rig.host.lastSettings?.dnsSettings?.servers, ["9.9.9.9"])
    }

    func testUnchangedSettingsAreNotAppliedAgain() {
        let rig = LifecycleRig().running(on: "wifi")
        rig.lifecycle.applyLive()
        XCTAssertEqual(rig.host.applyCount, 0)
    }

    func testNothingAppliesWhileTheTunnelIsDown() {
        let rig = LifecycleRig()
        rig.useSplitTunnel(["203.0.113.0/24"])
        rig.lifecycle.applyLive()
        XCTAssertEqual(rig.core.calls, [])
        XCTAssertEqual(rig.host.applyCount, 0)
    }
}
