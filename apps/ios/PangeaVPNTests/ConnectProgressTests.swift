import XCTest
@testable import PangeaVPN

@MainActor
final class ConnectProgressTests: XCTestCase {
    private let lines = [
        "Still connecting…",
        "This is taking longer than usual…",
        "Still working on it. We'll try other servers if needed.",
    ]

    func testStepsComeAtThreeEightAndFifteenSeconds() {
        XCTAssertEqual(ConnectProgress.steps.map(\.after), [.seconds(3), .seconds(8), .seconds(15)])
        XCTAssertEqual(ConnectProgress.steps.map(\.message), lines)
    }

    func testWaitsAreMeasuredFromTheStepBefore() {
        XCTAssertEqual(ConnectProgress.waits, [.seconds(3), .seconds(5), .seconds(7)])
    }

    func testMessageForAnElapsedTime() {
        XCTAssertNil(ConnectProgress.message(after: .milliseconds(2999)))
        XCTAssertEqual(ConnectProgress.message(after: .seconds(3)), lines[0])
        XCTAssertEqual(ConnectProgress.message(after: .seconds(10)), lines[1])
        XCTAssertEqual(ConnectProgress.message(after: .seconds(60)), lines[2])
    }

    func testProgressStepsThroughTheLinesWhileConnecting() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let clock = ManualSleeper()
        defer { clock.releaseAll() }
        let rig = await makeConnectionRig(defaults, sleeper: clock.sleeper)
        rig.tunnel.status = ConnectionFixtures.status("eu-2", .connecting)
        for (index, line) in lines.enumerated() {
            let waiting = await eventually { clock.requested.count == index + 1 }
            XCTAssertTrue(waiting)
            XCTAssertEqual(rig.store.progress, index == 0 ? nil : lines[index - 1])
            clock.fire()
            let shown = await eventually { rig.store.progress == line }
            XCTAssertTrue(shown, line)
        }
        XCTAssertEqual(clock.requested, [.seconds(3), .seconds(5), .seconds(7)])
        rig.tunnel.status = ConnectionFixtures.status("eu-2")
        XCTAssertNil(rig.store.progress)
    }

    func testProgressClearsAndStopsWhenConnectingEnds() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let clock = ManualSleeper()
        defer { clock.releaseAll() }
        let rig = await makeConnectionRig(defaults, sleeper: clock.sleeper)
        rig.tunnel.status = ConnectionFixtures.status("eu-2", .connecting)
        let started = await eventually { clock.requested.count == 1 }
        XCTAssertTrue(started)
        clock.fire()
        let shown = await eventually { rig.store.progress == lines[0] }
        XCTAssertTrue(shown)
        rig.tunnel.status = .disconnected
        XCTAssertNil(rig.store.progress)
        clock.fire()
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertNil(rig.store.progress)
        XCTAssertEqual(clock.requested.count, 2)
    }

    func testNoProgressOutsideConnecting() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let clock = ManualSleeper()
        defer { clock.releaseAll() }
        let rig = await makeConnectionRig(defaults, sleeper: clock.sleeper)
        for state in [TunnelState.connected, .disconnecting, .error, .disconnected] {
            rig.tunnel.status = ConnectionFixtures.status("eu-2", state)
        }
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertTrue(clock.requested.isEmpty)
        XCTAssertNil(rig.store.progress)
    }
}
