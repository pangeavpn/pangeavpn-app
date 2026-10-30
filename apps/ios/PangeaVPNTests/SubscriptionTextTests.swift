import XCTest
@testable import PangeaVPN

final class SubscriptionTextTests: XCTestCase {
    private let us = Locale(identifier: "en_US")
    private let utc = TimeZone(identifier: "UTC")!
    private let losAngeles = TimeZone(identifier: "America/Los_Angeles")!

    private func line(_ sub: Subscription?) -> SubscriptionLine {
        SubscriptionText.line(sub, locale: us, timeZone: utc)
    }

    private func sub(
        _ status: String, renews: Bool = false, expiresAt: String? = "2026-12-26T00:00:00Z",
        entitled: Bool? = true, cached: Bool = false
    ) -> Subscription {
        Subscription(status: status, renews: renews, expiresAt: expiresAt, entitled: entitled, cached: cached)
    }

    func testNoSubscription() {
        XCTAssertEqual(line(nil), SubscriptionLine(text: "No active subscription", warn: false))
    }

    func testActiveRenewing() {
        XCTAssertEqual(line(sub("active", renews: true)), SubscriptionLine(text: "Renews Dec 26, 2026", warn: false))
    }

    func testActiveEndingWithoutRenewal() {
        XCTAssertEqual(line(sub("active")), SubscriptionLine(text: "Expires Dec 26, 2026", warn: false))
    }

    func testTrialGetsThePrefix() {
        XCTAssertEqual(line(sub("trialing", renews: true)).text, "Free trial · Renews Dec 26, 2026")
        XCTAssertEqual(line(sub("trialing")).text, "Free trial · Expires Dec 26, 2026")
    }

    func testActiveWithoutDateDropsIt() {
        XCTAssertEqual(line(sub("active", renews: true, expiresAt: nil)).text, "Renews")
        XCTAssertEqual(line(sub("active", expiresAt: "not a date")).text, "Expires")
    }

    func testUnknownEntitlementDoesNotBlock() {
        XCTAssertEqual(line(sub("active", renews: true, entitled: nil)).text, "Renews Dec 26, 2026")
    }

    func testNotEntitledWinsOverActiveStatus() {
        let lapsed = sub("active", expiresAt: "2026-10-20T00:00:00Z", entitled: false)
        XCTAssertEqual(line(lapsed), SubscriptionLine(text: "Subscription expired \u{2014} Oct 20, 2026", warn: true))
    }

    func testNotEntitledWithoutDate() {
        XCTAssertEqual(line(sub("expired", expiresAt: nil, entitled: false)), SubscriptionLine(text: "Subscription expired", warn: true))
    }

    func testPastDue() {
        let due = sub("past_due", expiresAt: "2026-10-01T00:00:00Z")
        XCTAssertEqual(line(due), SubscriptionLine(text: "Payment past due \u{2014} Oct 1, 2026", warn: true))
        XCTAssertEqual(line(sub("past_due", expiresAt: nil)).text, "Payment past due")
    }

    func testOtherStatusesReadAsNone() {
        for status in ["canceled", "unpaid", "incomplete", "Active", ""] {
            XCTAssertEqual(line(sub(status)), SubscriptionLine(text: "No active subscription", warn: false), status)
        }
    }

    func testCachedAnswerSaysOffline() {
        XCTAssertEqual(line(sub("active", renews: true, cached: true)).text, "Renews Dec 26, 2026 (offline)")
        let lapsed = line(sub("active", entitled: false, cached: true))
        XCTAssertEqual(lapsed.text, "Subscription expired \u{2014} Dec 26, 2026 (offline)")
        XCTAssertTrue(lapsed.warn)
    }

    func testFractionalSecondsParse() {
        XCTAssertEqual(HubDate.medium("2026-12-26T10:15:30.123Z", locale: us, timeZone: utc), "Dec 26, 2026")
    }

    func testOffsetTimestampParses() {
        XCTAssertEqual(HubDate.medium("2026-12-26T01:00:00+02:00", locale: us, timeZone: utc), "Dec 25, 2026")
    }

    func testTimestampShowsTheReadersDay() {
        XCTAssertEqual(HubDate.medium("2026-10-01T03:00:00Z", locale: us, timeZone: losAngeles), "Sep 30, 2026")
    }

    func testBareDateKeepsItsDayInEveryZone() {
        XCTAssertEqual(HubDate.medium("2026-10-01", locale: us, timeZone: losAngeles), "Oct 1, 2026")
        XCTAssertEqual(HubDate.medium("2026-10-01", locale: us, timeZone: utc), "Oct 1, 2026")
    }

    func testUnreadableDatesAreNil() {
        XCTAssertNil(HubDate.medium(nil, locale: us, timeZone: utc))
        XCTAssertNil(HubDate.medium("", locale: us, timeZone: utc))
        XCTAssertNil(HubDate.medium("   ", locale: us, timeZone: utc))
        XCTAssertNil(HubDate.medium("yesterday", locale: us, timeZone: utc))
    }

    func testParseTrimsWhitespace() {
        XCTAssertNotNil(HubDate.parse(" 2026-12-26T00:00:00Z \n", timeZone: utc))
    }
}
