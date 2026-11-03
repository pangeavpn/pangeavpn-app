import XCTest
@testable import PangeaVPN

final class PanicWiperTests: XCTestCase {
    func testClearsEveryDefaultsDomain() throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        fixture.group.defaults.set(true, forKey: SharedPrefs.Key.killSwitch)
        fixture.group.defaults.set(["eu"], forKey: SharedPrefs.Key.recentRegions)
        fixture.standard.defaults.set("x", forKey: "someKey")
        fixture.wiper().run()
        XCTAssertNil(fixture.group.defaults.object(forKey: SharedPrefs.Key.killSwitch))
        XCTAssertNil(fixture.group.defaults.object(forKey: SharedPrefs.Key.recentRegions))
        XCTAssertNil(fixture.standard.defaults.object(forKey: "someKey"))
    }

    // Without the marker the next launch would wipe the Keychain again, signing out a fresh sign-in.
    func testRestoresTheInstallMarkerOnceTheKeychainIsEmpty() throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        fixture.standard.defaults.set(true, forKey: InstallMarker.key)
        fixture.wiper(keychainCleared: true).run()
        XCTAssertTrue(fixture.standard.defaults.bool(forKey: InstallMarker.key))
        XCTAssertNil(fixture.group.defaults.object(forKey: InstallMarker.key))
    }

    func testLeavesTheMarkerOffWhenTheKeychainWipeFails() throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        fixture.standard.defaults.set(true, forKey: InstallMarker.key)
        fixture.wiper(keychainCleared: false).run()
        XCTAssertNil(fixture.standard.defaults.object(forKey: InstallMarker.key))
    }

    func testDeletesContainerFilesButKeepsPreferences() throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        let removed = ["transport-memory.json", ".hidden", "logs/daemon.log", "Library/Caches/blob.bin"]
        for path in removed + ["Library/Preferences/group.plist"] {
            try fixture.write(path)
        }
        fixture.wiper().run()
        for path in removed {
            XCTAssertFalse(fixture.exists(path), path)
        }
        XCTAssertFalse(fixture.exists("logs"))
        XCTAssertFalse(fixture.exists("Library/Caches"))
        XCTAssertTrue(fixture.exists("Library/Preferences/group.plist"))
    }

    func testAMissingContainerIsFine() throws {
        let fixture = try WipeFixture()
        defer { fixture.cleanUp() }
        try FileManager.default.removeItem(at: fixture.container)
        fixture.group.defaults.set(true, forKey: SharedPrefs.Key.lockdown)
        fixture.wiper().run()
        XCTAssertNil(fixture.group.defaults.object(forKey: SharedPrefs.Key.lockdown))
    }

    func testLiveWiperTargetsTheAppGroup() {
        let live = PanicWiper.live()
        XCTAssertEqual(live.domains.first?.name, AppGroup.identifier)
        XCTAssertEqual(live.container, AppGroup.containerURL)
    }
}
