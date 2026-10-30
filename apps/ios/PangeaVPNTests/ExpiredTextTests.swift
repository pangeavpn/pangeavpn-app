import SwiftUI
import XCTest
@testable import PangeaVPN

final class ExpiredTextTests: XCTestCase {
    private let utc = TimeZone(identifier: "UTC")!
    private let english = Locale(identifier: "en_US")

    func testEntitledAnswerMeansTimeWasAdded() {
        XCTAssertEqual(ExpiredText.verdict(entitled: true, cached: false), .restored)
        XCTAssertEqual(ExpiredText.verdict(entitled: true, cached: true), .restored)
    }

    func testCachedAnswerMeansTheCheckDidNotReachTheHub() {
        XCTAssertEqual(ExpiredText.verdict(entitled: false, cached: true), .checkFailed)
    }

    func testLiveRefusalIsStillExpired() {
        XCTAssertEqual(ExpiredText.verdict(entitled: false, cached: false), .stillExpired)
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
