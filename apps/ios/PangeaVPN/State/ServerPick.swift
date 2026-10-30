import Foundation

// A connect with nothing picked goes to a random light node, so new users never herd onto one server.
enum ServerPick {
    static let preferredMaxLoad = 75

    static func random(from servers: [Server], roll: () -> Double = { Double.random(in: 0..<1) }) -> Server? {
        guard !servers.isEmpty else { return nil }
        let light = servers.filter { ($0.load ?? Int.max) <= preferredMaxLoad }
        let pool = light.isEmpty ? servers : light
        let value = roll()
        let scaled = value.isFinite ? (value * Double(pool.count)).rounded(.down) : 0
        // A roll of exactly 1 would index one past the end.
        return pool[Int(max(0, min(scaled, Double(pool.count - 1))))]
    }
}
