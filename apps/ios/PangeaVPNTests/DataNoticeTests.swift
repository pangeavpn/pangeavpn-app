import XCTest
@testable import PangeaVPN

final class DataNoticeTests: XCTestCase {
    private var suite: String?

    override func tearDown() {
        if let suite { UserDefaults.standard.removePersistentDomain(forName: suite) }
        super.tearDown()
    }

    private func freshDefaults() throws -> UserDefaults {
        let test = try makeTestDefaults()
        suite = test.suite
        return test.defaults
    }

    func testANewDeviceSeesTheNoticeUntilItIsAccepted() throws {
        let defaults = try freshDefaults()
        XCTAssertFalse(DataNotice.isAccepted(in: defaults))
        DataNotice.accept(in: defaults)
        XCTAssertTrue(DataNotice.isAccepted(in: defaults))
    }

    // A changed notice must be read again, so an older acceptance no longer counts.
    func testAnOlderVersionShowsTheNoticeAgain() throws {
        let defaults = try freshDefaults()
        defaults.set(DataNotice.version - 1, forKey: DataNotice.key)
        XCTAssertFalse(DataNotice.isAccepted(in: defaults))
    }
}
