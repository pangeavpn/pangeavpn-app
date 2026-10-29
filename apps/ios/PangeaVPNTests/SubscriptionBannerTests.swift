import XCTest
@testable import PangeaVPN

final class SubscriptionBannerTests: XCTestCase {
    private func sub(_ status: String) -> Subscription {
        Subscription(status: status, renews: false, expiresAt: nil)
    }

    func testShowsWhenSubscriptionIsMissing() {
        XCTAssertTrue(SubscriptionBanner.shouldShow(nil))
    }

    func testHiddenWhenActive() {
        XCTAssertFalse(SubscriptionBanner.shouldShow(sub("active")))
    }

    func testHiddenWhenTrialing() {
        XCTAssertFalse(SubscriptionBanner.shouldShow(sub("trialing")))
    }

    func testShownForOtherStatuses() {
        for status in ["canceled", "past_due", "unpaid", "incomplete", "expired", ""] {
            XCTAssertTrue(SubscriptionBanner.shouldShow(sub(status)), status)
        }
    }

    func testShownForUnknownStatus() {
        XCTAssertTrue(SubscriptionBanner.shouldShow(sub("something_new")))
    }

    func testStatusMatchIsCaseSensitiveLikeKotlin() {
        XCTAssertTrue(SubscriptionBanner.shouldShow(sub("Active")))
    }
}
