import XCTest
@testable import PangeaVPN

final class ReconnectPolicyTests: XCTestCase {
    func testFirstPathIsBaseline() {
        var policy = ReconnectPolicy()
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true), .ignore)
    }

    func testSameKeyIgnored() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true)
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true), .ignore)
    }

    func testNewKeyReconnectsAfterDebounce() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true)
        XCTAssertEqual(policy.pathChanged(key: "b", usable: true), .reconnect(after: 1.5))
        XCTAssertEqual(policy.pathChanged(key: "b", usable: true), .ignore)
    }

    func testUnusablePathWaits() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true)
        XCTAssertEqual(policy.pathChanged(key: "", usable: false), .waitForNetwork)
        XCTAssertEqual(policy.pathChanged(key: "b", usable: true), .reconnect(after: 1.5))
    }

    func testSameKeyAfterWaitingReconnects() {
        var policy = ReconnectPolicy()
        _ = policy.pathChanged(key: "a", usable: true)
        _ = policy.pathChanged(key: "a", usable: false)
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true), .reconnect(after: 1.5))
        XCTAssertEqual(policy.pathChanged(key: "a", usable: true), .ignore)
    }

    func testBackoff() {
        let policy = ReconnectPolicy()
        XCTAssertEqual([1, 2, 3].map { policy.backoff(attempt: $0) }, [3, 6, 9])
        XCTAssertEqual(policy.maxAttempts, 3)
    }
}
