import SwiftUI
import XCTest
@testable import PangeaVPN

final class ExpiredTextTests: XCTestCase {
    private let utc = TimeZone(identifier: "UTC")!
    private let english = Locale(identifier: "en_US")

    func testEntitledAnswerMeansTimeWasAdded() {
        XCTAssertEqual(ExpiredText.verdict(reached: true, entitled: true, cached: false), .restored)
        XCTAssertEqual(ExpiredText.verdict(reached: true, entitled: true, cached: true), .restored)
        XCTAssertEqual(ExpiredText.verdict(reached: false, entitled: true, cached: false), .restored)
    }

    func testCachedAnswerMeansTheCheckDidNotReachTheHub() {
        XCTAssertEqual(ExpiredText.verdict(reached: true, entitled: false, cached: true), .checkFailed)
    }

    func testNoAnswerMeansTheCheckFailed() {
        XCTAssertEqual(ExpiredText.verdict(reached: false, entitled: false, cached: false), .checkFailed)
    }

    func testLiveRefusalIsStillExpired() {
        XCTAssertEqual(ExpiredText.verdict(reached: true, entitled: false, cached: false), .stillExpired)
    }

    // The old, uncached expired answer stays in the store, so only the refresh result tells a failure apart.
    @MainActor
    func testAFailedRefreshReadsAsCheckFailedNotStillExpired() async {
        let core = FakeCore()
        core.restoreResult = .success(Session(email: "a@example.org", name: "A", servers: []))
        core.subscriptionResult = .success(Subscription(status: "expired", renews: false, expiresAt: nil, entitled: false))
        let session = SessionStore(core: core, tunnel: FakeTunnel())
        await session.restore()
        core.subscriptionResult = .failure(CoreError(message: "hub unreachable"))
        let reached = await session.refreshSubscription()
        let cached = session.subscription?.cached ?? false
        XCTAssertFalse(reached)
        XCTAssertEqual(ExpiredText.verdict(reached: reached, entitled: session.entitled, cached: cached), .checkFailed)
    }

    func testVerdictStyles() {
        XCTAssertEqual(ExpiredVerdict.restored.style, .success)
        XCTAssertEqual(ExpiredVerdict.stillExpired.style, .info)
        XCTAssertEqual(ExpiredVerdict.checkFailed.style, .warning)
    }

    func testVerdictMessagesDiffer() {
        let messages = Set([ExpiredVerdict.restored, .stillExpired, .checkFailed].map(\.message))
        XCTAssertEqual(messages.count, 3)
    }

    func testRanOutLineCarriesTheDate() throws {
        let line = try XCTUnwrap(ExpiredText.ranOut("2026-10-20T00:00:00Z", locale: english, timeZone: utc))
        XCTAssertTrue(line.contains("2026"), line)
        XCTAssertTrue(line.contains("Oct"), line)
        XCTAssertTrue(line.contains("20"), line)
    }

    func testRanOutAcceptsABareDate() {
        XCTAssertNotNil(ExpiredText.ranOut("2026-10-20", locale: english, timeZone: utc))
    }

    func testRanOutIsHiddenWithoutAReadableDate() {
        XCTAssertNil(ExpiredText.ranOut(nil, locale: english, timeZone: utc))
        XCTAssertNil(ExpiredText.ranOut("", locale: english, timeZone: utc))
        XCTAssertNil(ExpiredText.ranOut("soon", locale: english, timeZone: utc))
    }

    func testCheckHoldIsAtLeast700Milliseconds() {
        XCTAssertGreaterThanOrEqual(ExpiredText.checkHold, .milliseconds(700))
    }
}

final class RootScenePolicyTests: XCTestCase {
    func testActiveAndInactiveCountAsForeground() {
        XCTAssertTrue(RootScenePolicy.isForeground(.active))
        XCTAssertTrue(RootScenePolicy.isForeground(.inactive))
        XCTAssertFalse(RootScenePolicy.isForeground(.background))
    }

    func testRecordWritesTheSharedFlag() throws {
        let test = try makeTestDefaults()
        defer { test.defaults.removePersistentDomain(forName: test.suite) }
        RootScenePolicy.record(foreground: true, in: test.defaults)
        XCTAssertTrue(SharedPrefs.appInForeground(in: test.defaults))
        RootScenePolicy.record(foreground: false, in: test.defaults)
        XCTAssertFalse(SharedPrefs.appInForeground(in: test.defaults))
    }

    func testFirstCheckIsAlwaysDue() {
        XCTAssertTrue(RootScenePolicy.subscriptionDue(lastCheck: nil, now: Date(), entitled: true))
    }

    func testEntitledChecksAtMostEveryTenMinutes() {
        let now = Date()
        XCTAssertFalse(RootScenePolicy.subscriptionDue(lastCheck: now.addingTimeInterval(-599), now: now, entitled: true))
        XCTAssertTrue(RootScenePolicy.subscriptionDue(lastCheck: now.addingTimeInterval(-600), now: now, entitled: true))
    }

    func testExpiredChecksSoonerOnReturn() {
        let now = Date()
        XCTAssertFalse(RootScenePolicy.subscriptionDue(lastCheck: now.addingTimeInterval(-10), now: now, entitled: false))
        XCTAssertTrue(RootScenePolicy.subscriptionDue(lastCheck: now.addingTimeInterval(-31), now: now, entitled: false))
    }
}
