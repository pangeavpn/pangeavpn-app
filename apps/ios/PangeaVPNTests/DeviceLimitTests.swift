import XCTest
@testable import PangeaVPN

final class DeviceLimitTests: XCTestCase {
    func testMatchesDeviceLimitInAnyCase() {
        XCTAssertTrue(DeviceLimit.isDeviceLimit("Device limit reached"))
        XCTAssertTrue(DeviceLimit.isDeviceLimit("error: DEVICE LIMIT exceeded"))
    }

    func testMatchesDeviceLimitCode() {
        XCTAssertTrue(DeviceLimit.isDeviceLimit("403 DEVICE_LIMIT"))
        XCTAssertTrue(DeviceLimit.isDeviceLimit("device_limit"))
    }

    func testRejectsUnrelatedMessages() {
        XCTAssertFalse(DeviceLimit.isDeviceLimit("Invalid account number"))
        XCTAssertFalse(DeviceLimit.isDeviceLimit(""))
    }
}
