import XCTest
@testable import PangeaVPN

final class InstallMarkerTests: XCTestCase {
    func testMissingMarkerWipesAndPresentMarkerKeeps() {
        XCTAssertEqual(InstallMarker.action(markerPresent: false), .wipe)
        XCTAssertEqual(InstallMarker.action(markerPresent: true), .keep)
    }

    func testFirstLaunchWipesOnceThenSetsTheMarker() throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        var wipes = 0
        InstallMarker.apply(defaults: defaults) { wipes += 1; return true }
        InstallMarker.apply(defaults: defaults) { wipes += 1; return true }
        XCTAssertEqual(wipes, 1)
        XCTAssertTrue(defaults.bool(forKey: InstallMarker.key))
    }

    func testFailedWipeIsRetriedOnTheNextLaunch() throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        var wipes = 0
        InstallMarker.apply(defaults: defaults) { wipes += 1; return false }
        XCTAssertFalse(defaults.bool(forKey: InstallMarker.key))
        InstallMarker.apply(defaults: defaults) { wipes += 1; return true }
        XCTAssertEqual(wipes, 2)
        XCTAssertTrue(defaults.bool(forKey: InstallMarker.key))
    }

    func testExistingInstallNeverWipes() throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set(true, forKey: InstallMarker.key)
        var wipes = 0
        InstallMarker.apply(defaults: defaults) { wipes += 1; return true }
        XCTAssertEqual(wipes, 0)
    }
}
