import UIKit
import XCTest
@testable import PangeaVPN

@MainActor
final class PanicShortcutTests: XCTestCase {
    private var defaults: UserDefaults!
    private var suite = ""

    // XCTest declares setUp and tearDown nonisolated, but calls them on the main thread.
    override func setUpWithError() throws {
        let test = try makeTestDefaults()
        MainActor.assumeIsolated {
            defaults = test.defaults
            suite = test.suite
        }
    }

    override func tearDown() {
        MainActor.assumeIsolated {
            defaults.removePersistentDomain(forName: suite)
            UIApplication.shared.shortcutItems = []
            PanicTrigger.shared.requested = false
        }
    }

    private func otherItem() -> UIApplicationShortcutItem {
        UIApplicationShortcutItem(type: "org.pangeavpn.app.other", localizedTitle: "Other")
    }

    func testEnablingAddsOneNeutralWipeItem() {
        let items = PanicShortcut.items([], enabled: true)
        XCTAssertEqual(items.count, 1)
        XCTAssertEqual(items.first?.type, PanicShortcut.type)
        XCTAssertEqual(items.first?.localizedTitle, String(localized: "panic_shortcut_title", defaultValue: "Wipe data"))
        XCTAssertFalse(items.first?.localizedTitle.lowercased().contains("panic") ?? true)
        XCTAssertNotNil(items.first?.icon)
    }

    func testEnablingTwiceKeepsASingleItem() {
        let once = PanicShortcut.items([], enabled: true)
        let twice = PanicShortcut.items(once, enabled: true)
        XCTAssertEqual(twice.filter { PanicShortcut.isPanic($0) }.count, 1)
    }

    func testDisablingRemovesOnlyTheWipeItem() {
        let armed = PanicShortcut.items([otherItem()], enabled: true)
        XCTAssertEqual(armed.map { $0.type }, ["org.pangeavpn.app.other", PanicShortcut.type])
        let disarmed = PanicShortcut.items(armed, enabled: false)
        XCTAssertEqual(disarmed.map { $0.type }, ["org.pangeavpn.app.other"])
    }

    func testUpdateSetsAndClearsTheHomeScreenItem() {
        PanicShortcut.update(enabled: true)
        XCTAssertEqual(UIApplication.shared.shortcutItems?.map { $0.type }, [PanicShortcut.type])
        PanicShortcut.update(enabled: false)
        XCTAssertEqual(UIApplication.shared.shortcutItems?.map { $0.type } ?? [], [])
    }

    func testWipeItemRaisesTheTriggerAndRemovesItself() {
        let trigger = PanicTrigger()
        PanicShortcut.update(enabled: true)
        XCTAssertTrue(trigger.handle(PanicShortcut.makeItem()))
        XCTAssertTrue(trigger.requested)
        XCTAssertEqual(UIApplication.shared.shortcutItems?.map { $0.type } ?? [], [])
    }

    func testOtherItemsLeaveTheTriggerDown() {
        let trigger = PanicTrigger()
        XCTAssertFalse(trigger.handle(otherItem()))
        XCTAssertFalse(trigger.requested)
    }

    func testWipeNowRaisesTheSharedTrigger() {
        XCTAssertFalse(PanicTrigger.shared.requested)
        PanicTrigger.shared.request()
        XCTAssertTrue(PanicTrigger.shared.requested)
    }

    func testSummaryFollowsTheSetting() {
        let settings = SettingsStore(core: FakeCore(), defaults: defaults)
        XCTAssertEqual(SettingsSummary.panic(settings), SettingsSummary.off())
        settings.setPanicWipe(true)
        XCTAssertEqual(SettingsSummary.panic(settings), String(localized: "panic_summary_on", defaultValue: "Enabled"))
        XCTAssertNotEqual(SettingsSummary.panic(settings), SettingsSummary.off())
    }
}
