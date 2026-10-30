import XCTest
@testable import PangeaVPN

final class UpdateInfoTests: XCTestCase {
    private let receipts = URL(fileURLWithPath: "/private/var/containers/Bundle/Application/X/PangeaVPN.app/StoreKit")

    func testSandboxReceiptMeansTestFlight() {
        XCTAssertEqual(UpdateChannel(receiptURL: receipts.appendingPathComponent("sandboxReceipt")), .testFlight)
        XCTAssertEqual(UpdateChannel(receiptURL: receipts.appendingPathComponent("receipt")), .appStore)
        XCTAssertEqual(UpdateChannel(receiptURL: nil), .appStore)
    }

    func testBuildInfoReadsTheBundleKeys() {
        let info: [String: Any] = ["CFBundleShortVersionString": "0.8.2", "CFBundleVersion": "417"]
        let build = AppBuildInfo(info: info, receiptURL: nil)
        XCTAssertEqual(build.version, "0.8.2")
        XCTAssertEqual(build.build, "417")
        XCTAssertEqual(build.channel, .appStore)
    }

    func testMissingOrEmptyKeysFallBack() {
        let empty = AppBuildInfo(info: ["CFBundleShortVersionString": "", "CFBundleVersion": 3], receiptURL: nil)
        XCTAssertEqual(empty.version, "0.0.0")
        XCTAssertEqual(empty.build, "0")
        XCTAssertEqual(AppBuildInfo(info: nil, receiptURL: nil).version, "0.0.0")
    }

    @MainActor
    func testSummaryShowsTheBundleVersion() {
        XCTAssertEqual(SettingsSummary.update(), "v" + AppBuildInfo.current.version)
        XCTAssertFalse(AppBuildInfo.current.version.isEmpty)
    }
}
