import XCTest
@testable import PangeaVPN

final class LivenessPolicyTests: XCTestCase {
    private let wall: TimeInterval = 1_800_000_000

    private func handshake(agedBy seconds: TimeInterval) -> Int64 {
        Int64(wall - seconds)
    }

    func testNoHandshakeYetIsUnknown() {
        let policy = LivenessPolicy()
        XCTAssertEqual(policy.verdict(lastHandshake: 0, wallNow: wall, now: 1_000), .unknown)
    }

    func testStaleOnlyPastOneHundredEightySeconds() {
        let policy = LivenessPolicy()
        XCTAssertEqual(policy.verdict(lastHandshake: handshake(agedBy: 120), wallNow: wall, now: 0), .healthy)
        XCTAssertEqual(policy.verdict(lastHandshake: handshake(agedBy: 180), wallNow: wall, now: 0), .healthy)
        XCTAssertEqual(policy.verdict(lastHandshake: handshake(agedBy: 181), wallNow: wall, now: 0), .stale)
    }

    func testGraceHoldsForThirtySecondsOfMonotonicTime() {
        let policy = LivenessPolicy().armed(at: 100)
        let old = handshake(agedBy: 3_600)
        XCTAssertEqual(policy.verdict(lastHandshake: old, wallNow: wall, now: 100), .grace)
        XCTAssertEqual(policy.verdict(lastHandshake: old, wallNow: wall, now: 129.9), .grace)
        XCTAssertEqual(policy.verdict(lastHandshake: old, wallNow: wall, now: 130), .stale)
    }

    func testRearmingMovesTheGraceForward() {
        let first = LivenessPolicy().armed(at: 0)
        let second = first.armed(at: 50)
        XCTAssertEqual(first.graceUntil, 30)
        XCTAssertEqual(second.graceUntil, 80)
        XCTAssertEqual(second.verdict(lastHandshake: handshake(agedBy: 500), wallNow: wall, now: 60), .grace)
    }

    // A wake waits less than a start, so a tunnel that died in sleep is caught within seconds.
    func testWakeGraceIsShorterThanTheStartGrace() {
        let woke = LivenessPolicy().armed(at: 0, for: LivenessPolicy.wakeGrace)
        XCTAssertEqual(woke.graceUntil, 15)
        XCTAssertLessThan(LivenessPolicy.wakeGrace, LivenessPolicy.grace)
        XCTAssertGreaterThan(LivenessPolicy.wakeChecks.first ?? 0, LivenessPolicy.wakeGrace)
        XCTAssertEqual(woke.verdict(lastHandshake: handshake(agedBy: 3_600), wallNow: wall, now: 17), .stale)
    }

    func testWallClockDecidesAgeSoSleepCounts() {
        let policy = LivenessPolicy().armed(at: 0)
        let beforeSleep = handshake(agedBy: 30)
        XCTAssertEqual(policy.verdict(lastHandshake: beforeSleep, wallNow: wall, now: 40), .healthy)
        XCTAssertEqual(policy.verdict(lastHandshake: beforeSleep, wallNow: wall + 3_600, now: 40), .stale)
    }
}
