import Pangeacore
import XCTest

final class SmokeTests: XCTestCase {
    // The test host never opens a utun, so a linked core finds none.
    func testCoreLinks() {
        XCTAssertEqual(MobileFindTunFD(), -1)
    }
}
