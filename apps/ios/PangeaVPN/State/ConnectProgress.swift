import Foundation

// Reassurance under a slow connect, timed from when the tunnel started connecting.
enum ConnectProgress {
    struct Step: Equatable {
        let after: Duration
        let message: String
    }

    static var steps: [Step] {
        [
            Step(after: .seconds(3),
                 message: String(localized: "connect_progress_still_connecting", defaultValue: "Still connecting…")),
            Step(after: .seconds(8),
                 message: String(localized: "connect_progress_taking_longer", defaultValue: "This is taking longer than usual…")),
            Step(after: .seconds(15),
                 message: String(localized: "connect_progress_still_working", defaultValue: "Still working on it. We'll try other servers if needed.")),
        ]
    }

    // How long to wait before each step, measured from the one before it.
    static var waits: [Duration] {
        let offsets = steps.map(\.after)
        return zip(offsets, [Duration.zero] + offsets).map { $0 - $1 }
    }

    static func message(after elapsed: Duration) -> String? {
        steps.last { $0.after <= elapsed }?.message
    }
}
