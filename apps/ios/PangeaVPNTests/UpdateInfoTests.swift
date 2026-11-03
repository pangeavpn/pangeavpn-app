import XCTest
@testable import PangeaVPN

final class UpdateInfoTests: XCTestCase {
    func testExplanationPointsAtTheAppStoreOnly() {
        XCTAssertTrue(UpdateText.explanation.contains("App Store"))
        XCTAssertFalse(UpdateText.explanation.contains("TestFlight"))
    }

    func testBuildInfoReadsTheBundleKeys() {
        let info: [String: Any] = ["CFBundleShortVersionString": "0.8.2", "CFBundleVersion": "417"]
        let build = AppBuildInfo(info: info)
        XCTAssertEqual(build.version, "0.8.2")
        XCTAssertEqual(build.build, "417")
    }

    func testMissingOrEmptyKeysFallBack() {
        let empty = AppBuildInfo(info: ["CFBundleShortVersionString": "", "CFBundleVersion": 3])
        XCTAssertEqual(empty.version, "0.0.0")
        XCTAssertEqual(empty.build, "0")
        XCTAssertEqual(AppBuildInfo(info: nil).version, "0.0.0")
    }

    @MainActor
    func testSummaryShowsTheBundleVersion() {
        XCTAssertEqual(SettingsSummary.update(), "v" + AppBuildInfo.current.version)
        XCTAssertFalse(AppBuildInfo.current.version.isEmpty)
    }
}
