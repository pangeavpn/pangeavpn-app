import Foundation

// When Connect On Demand should be saved on; the controller is the only writer.
enum OnDemandPolicy {
    static func wanted(_ profile: TunnelProfile) -> Bool {
        profile.autoConnect || profile.lockdown
    }

    // An app connect still waiting for its first .connected must fail as an error, not retry on demand.
    static func reconciled(
        _ profile: TunnelProfile, state: TunnelState, enabled: Bool, awaitingFirstConnect: Bool
    ) -> Bool {
        guard wanted(profile) else { return false }
        if enabled { return true }
        switch state {
        case .connected: return true
        case .connecting: return !awaitingFirstConnect
        case .disconnected, .disconnecting, .error: return false
        }
    }

    // Another VPN taking over disables this configuration, which leaves its on-demand flag inert.
    static func active(configurationEnabled: Bool, onDemandEnabled: Bool) -> Bool {
        configurationEnabled && onDemandEnabled
    }
}
