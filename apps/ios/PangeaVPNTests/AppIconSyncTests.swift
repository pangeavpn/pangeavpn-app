import XCTest
@testable import PangeaVPN

@MainActor
private final class FakeAppIcon: AppIconSwitching {
    var supportsAlternateIcons = true
    var alternateIconName: String?
    var failure: Error?
    private(set) var requests: [String?] = []

    func setAlternateIconName(_ name: String?) async throws {
        requests.append(name)
        if let failure { throw failure }
        alternateIconName = name
    }
}

@MainActor
final class AppIconSyncTests: XCTestCase {
    func testEveryAccentButTerraHasItsOwnIcon() {
        XCTAssertNil(Accent.terra.iconName)
        let names = Accent.allCases.compactMap(\.iconName)
        XCTAssertEqual(names.count, Accent.allCases.count - 1)
        XCTAssertEqual(Set(names).count, names.count)
    }

    func testMatchingSwitchesToTheAccentsIcon() async {
        let icon = FakeAppIcon()
        let applied = await AppIconSync.apply(.ocean, matching: true, on: icon)
        XCTAssertTrue(applied)
        XCTAssertEqual(icon.requests, ["AppIcon-Ocean"])
        XCTAssertEqual(icon.alternateIconName, "AppIcon-Ocean")
    }

    func testNotMatchingRestoresThePrimaryIcon() async {
        let icon = FakeAppIcon()
        icon.alternateIconName = "AppIcon-Rose"
        await AppIconSync.apply(.rose, matching: false, on: icon)
        XCTAssertEqual(icon.requests, [nil])
        XCTAssertNil(icon.alternateIconName)
    }

    // Each change raises an iOS alert, so an icon already in place is left alone.
    func testAnIconAlreadyInPlaceIsNotSetAgain() async {
        let icon = FakeAppIcon()
        icon.alternateIconName = "AppIcon-Purple"
        let applied = await AppIconSync.apply(.purple, matching: true, on: icon)
        XCTAssertTrue(applied)
        XCTAssertTrue(icon.requests.isEmpty)
        let primary = FakeAppIcon()
        await AppIconSync.apply(.terra, matching: true, on: primary)
        XCTAssertTrue(primary.requests.isEmpty)
    }

    func testDevicesWithoutAlternateIconsAreLeftAlone() async {
        let icon = FakeAppIcon()
        icon.supportsAlternateIcons = false
        let applied = await AppIconSync.apply(.emerald, matching: true, on: icon)
        XCTAssertFalse(applied)
        XCTAssertTrue(icon.requests.isEmpty)
    }

    func testAFailedChangeSaysSo() async {
        let icon = FakeAppIcon()
        icon.failure = CoreError(message: "busy")
        let applied = await AppIconSync.apply(.emerald, matching: true, on: icon)
        XCTAssertFalse(applied)
        XCTAssertNil(icon.alternateIconName)
    }

    func testMatchingIsOnUntilTurnedOffAndThatSticks() throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let store = SettingsStore(core: FakeCore(), defaults: defaults)
        XCTAssertTrue(store.iconMatchesAccent)
        store.setIconMatchesAccent(false)
        XCTAssertFalse(SettingsStore(core: FakeCore(), defaults: defaults).iconMatchesAccent)
        store.resetToDefaults()
        XCTAssertTrue(store.iconMatchesAccent)
    }
}
