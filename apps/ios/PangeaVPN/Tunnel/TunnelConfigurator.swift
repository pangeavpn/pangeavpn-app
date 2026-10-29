import Foundation
import NetworkExtension

// Maps a TunnelProfile onto the saved VPN configuration, kept pure so tests can check every flag.
enum TunnelConfigurator {
    static let providerBundleIdentifier = "org.pangeavpn.app.tunnel"
    static let displayName = "PangeaVPN"

    // excludeAPNs, excludeCellularServices and excludeDeviceCommunication keep Apple's defaults.
    static func makeProtocol(_ profile: TunnelProfile) -> NETunnelProviderProtocol {
        let proto = NETunnelProviderProtocol()
        proto.providerBundleIdentifier = providerBundleIdentifier
        proto.serverAddress = displayName
        proto.includeAllNetworks = profile.strictKillSwitch
        proto.excludeLocalNetworks = profile.allowLan
        // iOS ignores enforceRoutes under includeAllNetworks.
        proto.enforceRoutes = !profile.strictKillSwitch
        proto.disconnectOnSleep = false
        return proto
    }

    static func onDemandRules() -> [NEOnDemandRule] {
        let rule = NEOnDemandRuleConnect()
        rule.interfaceTypeMatch = .any
        return [rule]
    }

    static func startOptions(serverId: String, candidates: [String]) -> [String: NSObject] {
        [
            TunnelStart.serverIdOption: serverId as NSString,
            TunnelStart.candidatesOption: TunnelStart.candidatesValue(candidates),
        ]
    }

    // Leaves isEnabled alone: only an explicit connect may take the VPN slot back from another app.
    static func apply(_ profile: TunnelProfile, onDemand: Bool, to manager: NETunnelProviderManager) {
        manager.protocolConfiguration = makeProtocol(profile)
        manager.localizedDescription = displayName
        manager.onDemandRules = onDemandRules()
        manager.isOnDemandEnabled = onDemand
    }

    // True when saving would change nothing, so a foreground reconcile skips a needless write.
    static func matches(
        _ proto: NEVPNProtocol?, rules: [NEOnDemandRule]?, onDemandEnabled: Bool,
        profile: TunnelProfile, onDemand: Bool
    ) -> Bool {
        guard let saved = proto as? NETunnelProviderProtocol else { return false }
        let wanted = makeProtocol(profile)
        return saved.providerBundleIdentifier == wanted.providerBundleIdentifier
            && saved.serverAddress == wanted.serverAddress
            && saved.includeAllNetworks == wanted.includeAllNetworks
            && saved.excludeLocalNetworks == wanted.excludeLocalNetworks
            && saved.enforceRoutes == wanted.enforceRoutes
            && saved.disconnectOnSleep == wanted.disconnectOnSleep
            && onDemandEnabled == onDemand
            && connectsOnAnyInterface(rules)
    }

    static func matches(_ manager: NETunnelProviderManager, profile: TunnelProfile, onDemand: Bool) -> Bool {
        matches(
            manager.protocolConfiguration, rules: manager.onDemandRules,
            onDemandEnabled: manager.isOnDemandEnabled, profile: profile, onDemand: onDemand)
    }

    static func connectsOnAnyInterface(_ rules: [NEOnDemandRule]?) -> Bool {
        guard let rules, rules.count == 1, let rule = rules.first as? NEOnDemandRuleConnect else { return false }
        return rule.interfaceTypeMatch == .any
    }

    static func isOurs(_ manager: NETunnelProviderManager) -> Bool {
        let proto = manager.protocolConfiguration as? NETunnelProviderProtocol
        return proto?.providerBundleIdentifier == providerBundleIdentifier
    }
}
