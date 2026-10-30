import XCTest
@testable import PangeaVPN

final class VersionTapUnlockTests: XCTestCase {
    private let start = Date(timeIntervalSince1970: 1_800_000_000)

    private func taps(_ offsets: [TimeInterval], from state: VersionTapUnlock = VersionTapUnlock()) -> [VersionTapUnlock] {
        var states: [VersionTapUnlock] = []
        var current = state
        for offset in offsets {
            current = current.tapped(at: start.addingTimeInterval(offset))
            states.append(current)
        }
        return states
    }

    func testFiveQuickTapsFireOnTheFifth() {
        let states = taps([0, 0.3, 0.6, 0.9, 1.2])
        XCTAssertEqual(states.map(\.fired), [false, false, false, false, true])
        XCTAssertEqual(states.prefix(4).map(\.count), [1, 2, 3, 4])
    }

    func testFiringResetsTheCount() {
        let fired = taps([0, 0.3, 0.6, 0.9, 1.2]).last
        XCTAssertEqual(fired?.count, 0)
        XCTAssertNil(fired?.lastTap)
        let next = fired?.tapped(at: start.addingTimeInterval(1.4))
        XCTAssertEqual(next?.count, 1)
        XCTAssertEqual(next?.fired, false)
    }

    func testFourTapsDoNotFire() {
        XCTAssertFalse(taps([0, 0.2, 0.4, 0.6]).contains { $0.fired })
    }

    func testGapExactlyAtTheWindowStillCounts() {
        let states = taps([0, 1.5, 3.0, 4.5, 6.0])
        XCTAssertEqual(states.last?.fired, true)
    }

    func testSlowTapRestartsTheRun() {
        let states = taps([0, 0.3, 0.6, 2.2, 2.4, 2.6, 2.8])
        XCTAssertEqual(states.map(\.count), [1, 2, 3, 1, 2, 3, 4])
        XCTAssertFalse(states.contains { $0.fired })
    }

    func testRestartedRunCanStillFire() {
        let states = taps([0, 0.3, 5, 5.3, 5.6, 5.9, 6.2])
        XCTAssertEqual(states.last?.fired, true)
    }

    func testClockGoingBackwardsRestartsTheRun() {
        let states = taps([10, 10.3, 9.0])
        XCTAssertEqual(states.map(\.count), [1, 2, 1])
    }

    func testEachRunTogglesOnce() {
        let offsets: [TimeInterval] = [0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9]
        XCTAssertEqual(taps(offsets).filter(\.fired).count, 2)
    }

    func testFreshStateIsEmpty() {
        let state = VersionTapUnlock()
        XCTAssertEqual(state.count, 0)
        XCTAssertNil(state.lastTap)
        XCTAssertFalse(state.fired)
    }
}
