import SwiftUI

struct DriftLook: Equatable {
    var spread: Double
    var alpha: Double
    var red: Double
    var green: Double
    var blue: Double

    // Desktop draws a connecting map in the chosen accent, not always terra.
    static var accent: (r: Double, g: Double, b: Double) { rgb(AccentTheme.shared.accent.hex) }
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

    static func rgb(_ hex: UInt32) -> (r: Double, g: Double, b: Double) {
        (Double((hex >> 16) & 0xFF) / 255, Double((hex >> 8) & 0xFF) / 255, Double(hex & 0xFF) / 255)
    }
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

    static func ease(_ raw: Double) -> Double {
        let p = min(max(raw, 0), 1)
        var t = p
        for _ in 0..<8 {
            let err = bezier(t, 0.4, 0.2) - p
            let slope = bezierSlope(t, 0.4, 0.2)
            if abs(err) < 1e-7 { return bezier(t, 0, 1) }
            if slope < 1e-6 { break }
            t = min(max(t - err / slope, 0), 1)
        }
        var lo = 0.0, hi = 1.0
        for _ in 0..<40 {
            t = (lo + hi) / 2
            if bezier(t, 0.4, 0.2) < p { lo = t } else { hi = t }
        }
        return bezier(t, 0, 1)
    }

    private static func bezier(_ t: Double, _ a: Double, _ b: Double) -> Double {
        let u = 1 - t
        return 3 * u * u * t * a + 3 * u * t * t * b + t * t * t
    }

    private static func bezierSlope(_ t: Double, _ a: Double, _ b: Double) -> Double {
        let u = 1 - t
        return 3 * u * u * a + 6 * u * t * (b - a) + 3 * t * t * (1 - b)
    }
}
