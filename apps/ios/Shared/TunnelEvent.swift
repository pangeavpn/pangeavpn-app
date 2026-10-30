import Foundation

// What the extension tells connection alerts as the tunnel comes up, drops, recovers or stops.
enum TunnelEvent: Equatable, Sendable {
    case connected
    case reconnecting
    case restored
    case blocked(TunnelStartError)
    case stopped
}
