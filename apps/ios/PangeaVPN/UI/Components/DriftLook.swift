import SwiftUI

struct DriftLook: Equatable {
    var spread: Double
    var alpha: Double
    var red: Double
    var green: Double
    var blue: Double

    static let accent = (r: 195.0 / 255, g: 86.0 / 255, b: 43.0 / 255)
    static let idle = (r: 132.0 / 255, g: 132.0 / 255, b: 124.0 / 255)
    static let connected = (r: 16.0 / 255, g: 185.0 / 255, b: 129.0 / 255)
    static let error = (r: 244.0 / 255, g: 63.0 / 255, b: 94.0 / 255)

    static func target(for state: TunnelState) -> DriftLook {
        let tint = tint(for: state)
        return DriftLook(spread: spread(for: state), alpha: mapAlpha(for: state), red: tint.r, green: tint.g, blue: tint.b)
    }

    static func spread(for state: TunnelState) -> Double {
        switch state {
        case .connected: return 0
        case .connecting: return 0.12
        case .disconnected, .disconnecting, .error: return 1
        }
    }

    static func tint(for state: TunnelState) -> (r: Double, g: Double, b: Double) {
        switch state {
        case .connected: return connected
        case .error: return error
        case .connecting: return accent
        case .disconnected, .disconnecting: return idle
        }
    }

    static func mapAlpha(for state: TunnelState) -> Double {
        switch state {
        case .connected: return 1
        case .connecting: return 0.85
        case .error: return 0.55
        case .disconnected, .disconnecting: return 0.5
        }
    }

    var tint: Color { Color(red: red, green: green, blue: blue) }
}

struct DriftTween {
    static let spreadDuration = 1.4
    static let tintDuration = 0.7

    let from: DriftLook
    let to: DriftLook
    let start: Date

    func look(at date: Date, animated: Bool = true) -> DriftLook {
        guard animated else { return to }
        let elapsed = date.timeIntervalSince(start)
        let slow = Self.ease(elapsed / Self.spreadDuration)
        let fast = Self.ease(elapsed / Self.tintDuration)
        return DriftLook(
            spread: Self.mix(from.spread, to.spread, slow),
            alpha: Self.mix(from.alpha, to.alpha, fast),
            red: Self.mix(from.red, to.red, fast),
            green: Self.mix(from.green, to.green, fast),
            blue: Self.mix(from.blue, to.blue, fast)
        )
    }

    private static func mix(_ a: Double, _ b: Double, _ t: Double) -> Double { a + (b - a) * t }

    private static func ease(_ raw: Double) -> Double {
        let t = min(max(raw, 0), 1)
        return t * t * (3 - 2 * t)
    }
}
