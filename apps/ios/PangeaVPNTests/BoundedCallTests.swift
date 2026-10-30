import XCTest
@testable import PangeaVPN

final class BoundedCallTests: XCTestCase {
    func testFinishedWork() async {
        let outcome = await BoundedCall.run(timeout: 5) {}
        XCTAssertEqual(outcome, .finished)
    }

    func testFailedWorkCarriesItsMessage() async {
        let outcome = await BoundedCall.run(timeout: 5) { throw CoreError(message: "hub unreachable") }
        guard case .failed(_, let message) = outcome else {
            return XCTFail("got \(outcome)")
        }
        XCTAssertEqual(message, "hub unreachable")
    }

    func testSlowWorkTimesOutWithoutWaiting() async {
        let gate = DispatchSemaphore(value: 0)
        let started = Date()
        let outcome = await BoundedCall.run(timeout: 0.1) { _ = gate.wait(timeout: .now() + 5) }
        let elapsed = Date().timeIntervalSince(started)
        gate.signal()
        XCTAssertEqual(outcome, .timedOut)
        XCTAssertLessThan(elapsed, 4)
    }
}
