import UserNotifications
import XCTest
@testable import PangeaVPN

private struct RequestFailure: Error {}

private actor FakeAuthorizer: NotificationAuthorizing {
    private(set) var status: UNAuthorizationStatus
    private(set) var requests = 0
    private let grants: Bool
    private let failure: Error?

    init(status: UNAuthorizationStatus, grants: Bool = true, failure: Error? = nil) {
        self.status = status
        self.grants = grants
        self.failure = failure
    }

    func authorizationStatus() async -> UNAuthorizationStatus {
        status
    }

    // Mirrors iOS: only an undetermined status ever shows the prompt.
    func requestAuthorization() async throws -> Bool {
        requests += 1
        if let failure { throw failure }
        guard status == .notDetermined else { return status == .authorized }
        status = grants ? .authorized : .denied
        return grants
    }
}

@MainActor
final class NotificationPermissionTests: XCTestCase {
    func testRefreshReadsTheSystemStatus() async {
        let permission = NotificationPermission(center: FakeAuthorizer(status: .denied))
        XCTAssertEqual(permission.status, .notDetermined)

        await permission.refresh()

        XCTAssertEqual(permission.status, .denied)
    }

    func testGrantedRequestAuthorizes() async {
        let center = FakeAuthorizer(status: .notDetermined)
        let permission = NotificationPermission(center: center)

        let granted = await permission.request()

        XCTAssertTrue(granted)
        XCTAssertEqual(permission.status, .authorized)
        let requests = await center.requests
        XCTAssertEqual(requests, 1)
    }

    func testDeclinedRequestEndsDenied() async {
        let permission = NotificationPermission(center: FakeAuthorizer(status: .notDetermined, grants: false))

        let granted = await permission.request()

        XCTAssertFalse(granted)
        XCTAssertEqual(permission.status, .denied)
    }

    func testAlreadyDeniedAnswersFalse() async {
        let permission = NotificationPermission(center: FakeAuthorizer(status: .denied))

        let granted = await permission.request()

        XCTAssertFalse(granted)
        XCTAssertEqual(permission.status, .denied)
    }

    func testAlreadyAuthorizedAnswersTrue() async {
        let permission = NotificationPermission(center: FakeAuthorizer(status: .authorized))

        let granted = await permission.request()

        XCTAssertTrue(granted)
        XCTAssertEqual(permission.status, .authorized)
    }

    func testFailedRequestAnswersFalseAndStillRefreshes() async {
        let center = FakeAuthorizer(status: .provisional, failure: RequestFailure())
        let permission = NotificationPermission(center: center)

        let granted = await permission.request()

        XCTAssertFalse(granted)
        XCTAssertEqual(permission.status, .provisional)
    }
}
