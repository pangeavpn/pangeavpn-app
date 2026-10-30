import NetworkExtension
import XCTest
@testable import PangeaVPN

final class TunnelConfiguratorTests: XCTestCase {
    private let allProfiles: [TunnelProfile] = (0..<16).map { bits in
        TunnelProfile(
            strictKillSwitch: bits & 1 != 0, allowLan: bits & 2 != 0,
            autoConnect: bits & 4 != 0, lockdown: bits & 8 != 0)
    }

    private func profile(strict: Bool = false, lan: Bool = false) -> TunnelProfile {
        TunnelProfile(strictKillSwitch: strict, allowLan: lan, autoConnect: true, lockdown: false)
    }

    // A default route under enforceRoutes drops every tunnelled packet on iPadOS 26 while showing connected.
    func testStandardProfileLeavesRoutesUnenforced() {
        let proto = TunnelConfigurator.makeProtocol(.standard)
        XCTAssertEqual(proto.providerBundleIdentifier, "org.pangeavpn.app.tunnel")
        XCTAssertEqual(proto.serverAddress, "PangeaVPN")
        XCTAssertFalse(proto.includeAllNetworks)
        XCTAssertFalse(proto.enforceRoutes)
        XCTAssertFalse(proto.excludeLocalNetworks)
        XCTAssertFalse(proto.disconnectOnSleep)
    }

    func testStrictKillSwitchIncludesAllNetworksAndDropsEnforceRoutes() {
        let proto = TunnelConfigurator.makeProtocol(profile(strict: true))
        XCTAssertTrue(proto.includeAllNetworks)
        XCTAssertFalse(proto.enforceRoutes)
        XCTAssertFalse(proto.disconnectOnSleep)
    }

    func testAllowLanExcludesLocalNetworksInBothModes() {
        for strict in [false, true] {
            XCTAssertTrue(TunnelConfigurator.makeProtocol(profile(strict: strict, lan: true)).excludeLocalNetworks)
            XCTAssertFalse(TunnelConfigurator.makeProtocol(profile(strict: strict, lan: false)).excludeLocalNetworks)
        }
    }

    func testOnDemandFlagsDoNotChangeTheProtocol() {
        for p in allProfiles {
            let proto = TunnelConfigurator.makeProtocol(p)
            XCTAssertEqual(proto.includeAllNetworks, p.strictKillSwitch, "\(p)")
            XCTAssertFalse(proto.enforceRoutes, "\(p)")
            XCTAssertEqual(proto.excludeLocalNetworks, p.allowLan, "\(p)")
        }
    }

    func testAppleDefaultsAreLeftAlone() {
        let defaults = NETunnelProviderProtocol()
        for p in allProfiles {
            let proto = TunnelConfigurator.makeProtocol(p)
            XCTAssertEqual(proto.excludeAPNs, defaults.excludeAPNs, "\(p)")
            XCTAssertEqual(proto.excludeCellularServices, defaults.excludeCellularServices, "\(p)")
            if #available(iOS 17.4, *) {
                XCTAssertEqual(proto.excludeDeviceCommunication, defaults.excludeDeviceCommunication, "\(p)")
            }
        }
    }

    func testOnDemandIsOneConnectRuleOnAnyInterface() throws {
        let rules = TunnelConfigurator.onDemandRules()
        XCTAssertEqual(rules.count, 1)
        let rule = try XCTUnwrap(rules.first as? NEOnDemandRuleConnect)
        XCTAssertEqual(rule.interfaceTypeMatch, .any)
        XCTAssertTrue(TunnelConfigurator.connectsOnAnyInterface(rules))
    }

    func testOtherRuleSetsAreNotTheConnectAnywhereRule() {
        let wifi = NEOnDemandRuleConnect()
        wifi.interfaceTypeMatch = .wiFi
        let cases: [[NEOnDemandRule]?] = [
            nil, [], [wifi], [NEOnDemandRuleDisconnect()],
            TunnelConfigurator.onDemandRules() + TunnelConfigurator.onDemandRules(),
        ]
        for rules in cases {
            XCTAssertFalse(TunnelConfigurator.connectsOnAnyInterface(rules), "\(String(describing: rules))")
        }
    }

    func testStartOptionsCarryTheServerAndItsFallbacks() {
        let options = TunnelConfigurator.startOptions(serverId: "fra-1", candidates: ["fra-1", "fra-2", "ams-1"])
        XCTAssertEqual(options[TunnelStart.serverIdOption] as? String, "fra-1")
        XCTAssertEqual(
            TunnelStart.resolveCandidates(options: options, serverId: "fra-1"), ["fra-1", "fra-2", "ams-1"])
    }

    func testFreshConfigurationMatchesItsOwnProfile() {
        for p in allProfiles {
            for onDemand in [false, true] {
                XCTAssertTrue(TunnelConfigurator.matches(
                    TunnelConfigurator.makeProtocol(p), rules: TunnelConfigurator.onDemandRules(),
                    onDemandEnabled: onDemand, profile: p, onDemand: onDemand), "\(p) onDemand \(onDemand)")
            }
        }
    }

    func testAnyChangedFlagNeedsASave() {
        let saved = profile()
        let proto = TunnelConfigurator.makeProtocol(saved)
        let rules = TunnelConfigurator.onDemandRules()
        for changed in [profile(strict: true), profile(lan: true)] {
            XCTAssertFalse(TunnelConfigurator.matches(
                proto, rules: rules, onDemandEnabled: false, profile: changed, onDemand: false), "\(changed)")
        }
        XCTAssertFalse(TunnelConfigurator.matches(
            proto, rules: rules, onDemandEnabled: false, profile: saved, onDemand: true))
        XCTAssertFalse(TunnelConfigurator.matches(
            proto, rules: nil, onDemandEnabled: false, profile: saved, onDemand: false))
    }

    // A profile saved by a build that still enforced routes is rewritten at the next reconcile.
    func testAProfileThatEnforcesRoutesNeedsASave() {
        let stale = TunnelConfigurator.makeProtocol(.standard)
        stale.enforceRoutes = true
        XCTAssertFalse(TunnelConfigurator.matches(
            stale, rules: TunnelConfigurator.onDemandRules(), onDemandEnabled: false,
            profile: .standard, onDemand: false))
    }

    func testForeignOrMissingProtocolNeedsASave() {
        let rules = TunnelConfigurator.onDemandRules()
        XCTAssertFalse(TunnelConfigurator.matches(
            nil, rules: rules, onDemandEnabled: false, profile: .standard, onDemand: false))
        XCTAssertFalse(TunnelConfigurator.matches(
            NEVPNProtocolIKEv2(), rules: rules, onDemandEnabled: false, profile: .standard, onDemand: false))
        let other = TunnelConfigurator.makeProtocol(.standard)
        other.providerBundleIdentifier = "com.example.other"
        XCTAssertFalse(TunnelConfigurator.matches(
            other, rules: rules, onDemandEnabled: false, profile: .standard, onDemand: false))
    }

    func testSleepDisconnectNeedsASave() {
        let proto = TunnelConfigurator.makeProtocol(.standard)
        proto.disconnectOnSleep = true
        XCTAssertFalse(TunnelConfigurator.matches(
            proto, rules: TunnelConfigurator.onDemandRules(), onDemandEnabled: false,
            profile: .standard, onDemand: false))
    }
}
