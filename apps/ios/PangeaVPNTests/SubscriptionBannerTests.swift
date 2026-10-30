import XCTest
@testable import PangeaVPN

@MainActor
final class SubscriptionBannerTests: XCTestCase {
    private func sub(_ status: String, expiresAt: String? = nil, entitled: Bool? = nil) -> Subscription {
        Subscription(status: status, renews: false, expiresAt: expiresAt, entitled: entitled)
    }

    func testHiddenWhileTheSubscriptionIsUnknown() {
        XCTAssertFalse(SubscriptionBanner.shouldShow(nil))
    }

    func testHiddenWhenActiveOrTrialing() {
        XCTAssertFalse(SubscriptionBanner.shouldShow(sub("active")))
        XCTAssertFalse(SubscriptionBanner.shouldShow(sub("trialing")))
    }

    func testShownForOtherStatuses() {
        for status in ["canceled", "past_due", "unpaid", "incomplete", "expired", "", "something_new"] {
            XCTAssertTrue(SubscriptionBanner.shouldShow(sub(status)), status)
        }
    }

    func testStatusMatchIsCaseSensitive() {
        XCTAssertTrue(SubscriptionBanner.shouldShow(sub("Active")))
    }

    // The expired panel speaks for a plan the hub says is over, so the banner stays out of its way.
    func testHiddenWhenNotEntitled() {
        XCTAssertFalse(SubscriptionBanner.shouldShow(sub("expired", entitled: false)))
        XCTAssertTrue(SubscriptionBanner.shouldShow(sub("canceled", entitled: true)))
    }

    func testOnlyPastDueWarns() {
        XCTAssertTrue(SubscriptionBanner.warns(sub("past_due")))
        XCTAssertFalse(SubscriptionBanner.warns(sub("canceled")))
        XCTAssertFalse(SubscriptionBanner.warns(nil))
    }

    func testTextWithoutADate() {
        XCTAssertEqual(SubscriptionBanner.text(sub("canceled")), "No active subscription")
        XCTAssertEqual(SubscriptionBanner.text(sub("canceled", expiresAt: "not a date")), "No active subscription")
        XCTAssertEqual(SubscriptionBanner.text(nil), "No active subscription")
    }

    func testTextFormatsTheExpiryDate() {
        let text = SubscriptionBanner.text(sub("canceled", expiresAt: "2026-12-01T00:00:00Z"))
        XCTAssertTrue(text.hasPrefix("Expires "), text)
        XCTAssertFalse(text.contains("T00:00:00Z"), text)
    }
}
