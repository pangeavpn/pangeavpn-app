import Foundation

enum HeroState {
    // An in-place switch keeps the tunnel up, so only the store knows one is under way.
    static func display(_ state: TunnelState, busy: Bool, switching: Bool) -> TunnelState {
        if switching && state == .connected { return .connecting }
        if busy && (state == .disconnected || state == .error) { return .connecting }
        return state
    }

    // A tunnel recovering after it was up keeps its blocking routes, so the internet stays paused.
    static func reconnecting(_ state: TunnelState, busy: Bool, sawConnected: Bool, lastHandshake: Int64) -> Bool {
        state == .connecting && !busy && (sawConnected || lastHandshake > 0)
    }
}

enum HeroDetailText {
    static var pausedLine: String {
        String(localized: "main_paused_detail", defaultValue: "Internet is paused until the VPN is back.")
    }

    // Only an idle hero has room for a connect error; anything later goes to a toast.
    static func carriesError(_ display: TunnelState) -> Bool {
        display == .disconnected || display == .error
    }

    static func resolve(display: TunnelState, statusDetail: String, serverName: String,
                        error: String?, progress: String?, paused: Bool) -> String {
        if paused { return pausedLine }
        if display == .connecting, let progress, !progress.isEmpty { return progress }
        if carriesError(display), let error, !error.isEmpty { return error }
        return statusDetail.isEmpty ? serverName : statusDetail
    }
}

enum HeroChips {
    static func showsPostQuantum(display: TunnelState, postQuantum: Bool) -> Bool {
        display == .connected && postQuantum
    }

    // iOS ignores excluded routes under includeAllNetworks, so the strict kill switch voids the split.
    static func showsSplit(display: TunnelState, enabled: Bool, ranges: [String], strictKillSwitch: Bool) -> Bool {
        display == .connected && enabled && !ranges.isEmpty && !strictKillSwitch
    }
}

enum MainAction: Equatable {
    case connect, provisioning, stop, disconnect, disconnecting, lockdown, hidden

    var isButton: Bool { self != .lockdown && self != .hidden }

    // Lockdown hides every way to stop the tunnel; the store ignores a tap while a start is in flight.
    static func resolve(state: TunnelState, busy: Bool, switching: Bool, reconnecting: Bool,
                        lockdownBlocks: Bool, entitled: Bool) -> MainAction {
        switch state {
        case .disconnecting:
            return .disconnecting
        case .connected, .connecting:
            if lockdownBlocks { return .lockdown }
            if busy { return .provisioning }
            if reconnecting || (state == .connected && !switching) { return .disconnect }
            return .stop
        case .disconnected, .error:
            if busy { return .provisioning }
            return entitled ? .connect : .hidden
        }
    }
}
