import XCTest
@testable import PangeaVPN

@MainActor
final class ToastCenterTests: XCTestCase {
    func testNewestToastReplacesTheOld() {
        let center = ToastCenter()
        center.show("first")
        center.show("second", style: .success)
        XCTAssertEqual(center.current?.text, "second")
        XCTAssertEqual(center.current?.style, .success)
    }

    func testDismissIgnoresAStaleId() {
        let center = ToastCenter()
        center.show("first")
        let stale = center.current?.id
        center.show("second")
        center.dismiss(stale)
        XCTAssertEqual(center.current?.text, "second")
        center.dismiss()
        XCTAssertNil(center.current)
    }

    // A full-screen cover's overlay attaches after the app's, so only the cover's draws.
    func testOnlyTheNewestHostDraws() {
        let center = ToastCenter()
        let root = UUID()
        let cover = UUID()
        center.attach(root)
        center.attach(cover)
        XCTAssertFalse(center.isTopHost(root))
        XCTAssertTrue(center.isTopHost(cover))
        center.detach(cover)
        XCTAssertTrue(center.isTopHost(root))
    }

    func testReattachingMovesAHostToTheTop() {
        let center = ToastCenter()
        let a = UUID()
        let b = UUID()
        center.attach(a)
        center.attach(b)
        center.attach(a)
        XCTAssertTrue(center.isTopHost(a))
        XCTAssertEqual(center.hosts.count, 2)
    }
}
