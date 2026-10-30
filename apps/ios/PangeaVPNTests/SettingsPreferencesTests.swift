import SwiftUI
import XCTest
@testable import PangeaVPN

@MainActor
final class SettingsPreferencesTests: XCTestCase {
    private var defaults: UserDefaults!
    private var suite = ""

    override func setUpWithError() throws {
        let test = try makeTestDefaults()
        defaults = test.defaults
        suite = test.suite
    }

    override func tearDown() {
        defaults.removePersistentDomain(forName: suite)
        AccentTheme.shared.accent = .terra
    }

    func testFreshInstallDefaults() {
        let store = SettingsStore(core: FakeCore(), defaults: defaults)
        XCTAssertFalse(store.killSwitch)
        XCTAssertTrue(store.autoConnect)
        XCTAssertFalse(store.lockdown)
        XCTAssertTrue(store.connectionAlerts)
        XCTAssertFalse(store.panicWipe)
        XCTAssertEqual(store.accent, .terra)
        XCTAssertFalse(store.developerMode)
        XCTAssertFalse(store.splitTunnelEnabled)
        XCTAssertEqual(store.splitTunnelRanges, [])
        XCTAssertFalse(store.multihopEnabled)
        XCTAssertNil(store.multihopEntryId)
        XCTAssertEqual(store.tunnelProfile, .standard)
    }

    func testEveryPreferenceSurvivesARelaunch() {
        let store = SettingsStore(core: FakeCore(), defaults: defaults)
        store.setKillSwitch(true)
        store.setAutoConnect(false)
        store.setLockdown(true)
        store.setConnectionAlerts(false)
        store.setPanicWipe(true)
        store.setAccent(.ocean)
        store.setDeveloperMode(true)
        store.setSplitTunnelEnabled(true)
        store.setSplitTunnelRanges(["192.0.2.0/24"])
        store.setMultihopEnabled(true)
        store.setMultihopEntry("de-fra-1")

        let reopened = SettingsStore(core: FakeCore(), defaults: defaults)
        XCTAssertTrue(reopened.killSwitch)
        XCTAssertFalse(reopened.autoConnect)
        XCTAssertTrue(reopened.lockdown)
        XCTAssertFalse(reopened.connectionAlerts)
        XCTAssertTrue(reopened.panicWipe)
        XCTAssertEqual(reopened.accent, .ocean)
        XCTAssertTrue(reopened.developerMode)
        XCTAssertTrue(reopened.splitTunnelEnabled)
        XCTAssertEqual(reopened.splitTunnelRanges, ["192.0.2.0/24"])
        XCTAssertTrue(reopened.multihopEnabled)
        XCTAssertEqual(reopened.multihopEntryId, "de-fra-1")
    }

    func testTunnelProfileFollowsTheSettings() async {
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        store.setKillSwitch(true)
        store.setLockdown(true)
        await store.setAllowLan(true)
        XCTAssertEqual(store.tunnelProfile,
                       TunnelProfile(strictKillSwitch: true, allowLan: true, autoConnect: true, lockdown: true))
    }

    func testAccentDrivesThePalette() {
        let store = SettingsStore(core: FakeCore(), defaults: defaults)
        store.setAccent(.rose)
        XCTAssertEqual(AccentTheme.shared.accent, .rose)
        XCTAssertEqual(rgba(Palette.accent), rgba(Accent.rose.color))
        XCTAssertNotEqual(rgba(Palette.accent), rgba(Accent.terra.color))
    }

    private func rgba(_ color: Color) -> [CGFloat] {
        var r: CGFloat = 0, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 0
        UIColor(color).getRed(&r, green: &g, blue: &b, alpha: &a)
        return [r, g, b, a].map { ($0 * 255).rounded() }
    }

    func testUnknownStoredAccentFallsBackToTerra() {
        defaults.set("neon", forKey: SharedPrefs.Key.accent)
        XCTAssertEqual(SettingsStore(core: FakeCore(), defaults: defaults).accent, .terra)
    }

    // The extension reads these through SharedPrefs, so the app's setters must land there.
    func testExtensionSeesSplitTunnelAndMultihopChoices() {
        let store = SettingsStore(core: FakeCore(), defaults: defaults)
        store.setSplitTunnelRanges(["198.51.100.7"])
        XCTAssertEqual(SharedPrefs.excludedRanges(in: defaults), [])
        store.setSplitTunnelEnabled(true)
        XCTAssertEqual(SharedPrefs.excludedRanges(in: defaults), ["198.51.100.7"])

        XCTAssertNil(SharedPrefs.multihopEntry(in: defaults))
        store.setMultihopEnabled(true)
        XCTAssertEqual(SharedPrefs.multihopEntry(in: defaults), "")
        store.setMultihopEntry("nl-ams-1")
        XCTAssertEqual(SharedPrefs.multihopEntry(in: defaults), "nl-ams-1")
        store.setMultihopEntry(nil)
        XCTAssertEqual(SharedPrefs.multihopEntry(in: defaults), "")
        XCTAssertNil(store.multihopEntryId)
    }

    func testConnectionAlertsDefaultOnForTheExtension() {
        XCTAssertTrue(SharedPrefs.connectionAlerts(in: defaults))
        SettingsStore(core: FakeCore(), defaults: defaults).setConnectionAlerts(false)
        XCTAssertFalse(SharedPrefs.connectionAlerts(in: defaults))
    }

    func testResetToDefaultsClearsEverythingInMemory() async {
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        store.setKillSwitch(true)
        store.setAccent(.emerald)
        store.setDeveloperMode(true)
        store.setMultihopEnabled(true)
        await store.setPostQuantum(true)
        store.error = "stale"

        store.resetToDefaults()
        XCTAssertFalse(store.killSwitch)
        XCTAssertEqual(store.accent, .terra)
        XCTAssertEqual(AccentTheme.shared.accent, .terra)
        XCTAssertFalse(store.developerMode)
        XCTAssertFalse(store.multihopEnabled)
        XCTAssertEqual(store.settings, AppSettings())
        XCTAssertFalse(store.loaded)
        XCTAssertNil(store.error)
    }

    func testPostQuantumGoesThroughTheCore() async {
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        await store.setPostQuantum(true)
        XCTAssertTrue(store.settings.postQuantum)
        XCTAssertEqual(core.savedSettings.last?.postQuantum, true)
    }

    func testHubTestFailureReadsAsAFailedProbe() async {
        let core = FakeCore()
        core.hubTestResults["reality"] = .failure(CoreError(message: "unknown hub method"))
        let store = SettingsStore(core: core, defaults: defaults)
        let result = await store.testHubMethod("reality")
        XCTAssertFalse(result.ok)
        XCTAssertEqual(result.method, "reality")
        XCTAssertEqual(result.detail, "unknown hub method")
    }

    func testHubStatusComesFromTheCore() async {
        let core = FakeCore()
        core.hubStatusValue = HubStatus(active: "fronted", detail: nil)
        let status = await SettingsStore(core: core, defaults: defaults).hubStatus()
        XCTAssertEqual(status.active, "fronted")
    }
}
