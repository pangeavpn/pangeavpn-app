import Foundation

// Desktop's hidden switch: five taps on the version, each within 1.5 s of the last.
struct VersionTapUnlock: Equatable, Sendable {
    static let tapsNeeded = 5
    static let window: TimeInterval = 1.5

    let count: Int
    let lastTap: Date?
    // True on the tap that completes the run; the next tap starts a fresh count.
    let fired: Bool

    init() {
        self.init(count: 0, lastTap: nil, fired: false)
    }

    private init(count: Int, lastTap: Date?, fired: Bool) {
        self.count = count
        self.lastTap = lastTap
        self.fired = fired
    }

    func tapped(at now: Date) -> VersionTapUnlock {
        let next = continues(at: now) ? count + 1 : 1
        if next >= Self.tapsNeeded {
            return VersionTapUnlock(count: 0, lastTap: nil, fired: true)
        }
        return VersionTapUnlock(count: next, lastTap: now, fired: false)
    }

    // A clock that moved backwards breaks the run rather than extending it.
    private func continues(at now: Date) -> Bool {
        guard let lastTap else { return false }
        let gap = now.timeIntervalSince(lastTap)
        return gap >= 0 && gap <= Self.window
    }
}
