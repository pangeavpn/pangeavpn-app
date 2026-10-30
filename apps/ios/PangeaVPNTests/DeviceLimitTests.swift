import XCTest
@testable import PangeaVPN

final class DeviceLimitTests: XCTestCase {
    private let utc = TimeZone(identifier: "UTC")!
    private let english = Locale(identifier: "en_US")

    func testMatchesDeviceLimitInAnyCase() {
        XCTAssertTrue(DeviceLimit.isDeviceLimit("Device limit reached"))
        XCTAssertTrue(DeviceLimit.isDeviceLimit("error: DEVICE LIMIT exceeded"))
    }

    func testMatchesDeviceLimitCode() {
        XCTAssertTrue(DeviceLimit.isDeviceLimit("403 DEVICE_LIMIT"))
        XCTAssertTrue(DeviceLimit.isDeviceLimit("device_limit"))
    }

    func testUppercaseMessageMatches() {
        XCTAssertTrue(DeviceLimit.isDeviceLimit("DEVICE LIMIT REACHED"))
        XCTAssertTrue(DeviceLimit.isDeviceLimit("DEVICE_LIMIT"))
    }

    func testRejectsUnrelatedMessages() {
        XCTAssertFalse(DeviceLimit.isDeviceLimit("Invalid account number"))
        XCTAssertFalse(DeviceLimit.isDeviceLimit(""))
    }

    func testNameUsesTrimmedFriendlyName() {
        let device = Device(id: "a", friendlyName: "  Work laptop ", createdAt: "", status: "active")
        XCTAssertEqual(DeviceLimitText.name(device), "Work laptop")
    }

    func testMissingOrBlankNameReadsUnnamed() {
        let missing = Device(id: "c0ffee00-1234", friendlyName: nil, createdAt: "", status: "active")
        let blank = Device(id: "b", friendlyName: "   ", createdAt: "", status: "active")
        XCTAssertEqual(DeviceLimitText.name(missing), DeviceLimitText.unnamed)
        XCTAssertEqual(DeviceLimitText.name(blank), DeviceLimitText.unnamed)
    }

    func testAddedLineCarriesTheDate() throws {
        let device = Device(id: "a", friendlyName: "x", createdAt: "2026-10-02T19:45:00Z", status: "active")
        let line = try XCTUnwrap(DeviceLimitText.added(device, locale: english, timeZone: utc))
        XCTAssertTrue(line.contains("2026"), line)
        XCTAssertTrue(line.contains("Oct"), line)
        XCTAssertTrue(line.contains("2"), line)
    }

    func testAddedLineAcceptsFractionalSeconds() {
        let device = Device(id: "a", friendlyName: "x", createdAt: "2026-09-01T10:00:00.123Z", status: "active")
        XCTAssertNotNil(DeviceLimitText.added(device, locale: english, timeZone: utc))
    }

    func testAddedLineIsOmittedForAnUnreadableDate() {
        let empty = Device(id: "a", friendlyName: "x", createdAt: "", status: "active")
        let junk = Device(id: "b", friendlyName: "x", createdAt: "yesterday", status: "active")
        XCTAssertNil(DeviceLimitText.added(empty, locale: english, timeZone: utc))
        XCTAssertNil(DeviceLimitText.added(junk, locale: english, timeZone: utc))
    }

    func testRetrySignedIn() {
        XCTAssertEqual(DeviceLimitText.retryOutcome(signedIn: true, showingLimit: false, loginError: nil), .signedIn)
    }

    func testRetryStillAtLimitWhenTheScreenStaysWithoutAnError() {
        XCTAssertEqual(DeviceLimitText.retryOutcome(signedIn: false, showingLimit: true, loginError: nil), .stillAtLimit)
    }

    func testRetryFailedWhenTheStoreReportsAnError() {
        XCTAssertEqual(
            DeviceLimitText.retryOutcome(signedIn: false, showingLimit: true, loginError: "hub unreachable"),
            .failed)
    }

    func testNoticesHaveText() {
        for notice in [DeviceLimitNotice.loadFailed, .removeFailed, .stillAtLimit] {
            XCTAssertFalse(notice.text.isEmpty)
        }
        XCTAssertNotEqual(DeviceLimitNotice.removeFailed.text, DeviceLimitNotice.stillAtLimit.text)
    }
}
