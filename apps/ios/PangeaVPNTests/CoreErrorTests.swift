import XCTest
@testable import PangeaVPN

final class CoreErrorTests: XCTestCase {
    func testCoreErrorUsesGoMessage() {
        let error = NSError(
            domain: "go", code: 1,
            userInfo: [NSLocalizedDescriptionKey: "mobile: not authenticated"])
        XCTAssertEqual(CoreError.from(error).message, "mobile: not authenticated")
    }

    func testCoreErrorNilIsUnknown() {
        XCTAssertEqual(CoreError.from(nil).message, "Unknown error")
    }
}
