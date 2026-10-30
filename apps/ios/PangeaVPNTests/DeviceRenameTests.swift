import XCTest
@testable import PangeaVPN

private func device(_ id: String, current: Bool = false) -> Device {
    Device(id: id, friendlyName: "iOS", createdAt: "2026-01-01T00:00:00Z", status: "active", current: current)
}

@MainActor
final class DeviceRenameTests: XCTestCase {
    private func loadedStore(_ core: FakeCore) async -> SessionStore {
        core.devicesResult = .success([device("d1", current: true), device("d2")])
        let store = SessionStore(core: core, tunnel: FakeTunnel())
        await store.loadDevices()
        return store
    }

    func testValidationMatchesTheHub() {
        let cases: [(String, String?)] = [
            (" Work Phone ", "Work Phone"),
            ("", nil),
            ("   \n", nil),
            (String(repeating: "a", count: 64), String(repeating: "a", count: 64)),
            (String(repeating: "a", count: 65), nil),
            (String(repeating: "\u{E9}", count: 64), String(repeating: "\u{E9}", count: 64)),
            (String(repeating: "😀", count: 32), String(repeating: "😀", count: 32)),
            (String(repeating: "😀", count: 33), nil),
            ("\u{FEFF}Tab\u{FEFF}", "Tab"),
            ("\u{85}x", "\u{85}x"),
        ]
        for (raw, expected) in cases {
            XCTAssertEqual(DeviceRename.validated(raw), expected, raw.debugDescription)
        }
    }

    func testRenameStoresTheTrimmedNameAndKeepsTheRest() async {
        let core = FakeCore()
        let store = await loadedStore(core)
        let ok = await store.renameDevice("d1", to: "  Work Phone  ")
        XCTAssertTrue(ok)
        XCTAssertEqual(core.calls.last, "renameDevice(d1,Work Phone)")
        XCTAssertEqual(store.devices.map(\.friendlyName), ["Work Phone", "iOS"])
        XCTAssertEqual(store.devices.map(\.current), [true, false])
        XCTAssertNil(store.deviceError)
    }

    func testRenameShowsTheNameTheHubStored() async {
        let core = FakeCore()
        let store = await loadedStore(core)
        core.renameResult = .success("Hub Name")
        let ok = await store.renameDevice("d2", to: "Mine")
        XCTAssertTrue(ok)
        XCTAssertEqual(store.devices.map(\.friendlyName), ["iOS", "Hub Name"])
    }

    func testOutOfRangeNamesNeverReachTheCore() async {
        for name in ["   ", String(repeating: "😀", count: 33)] {
            let core = FakeCore()
            let store = await loadedStore(core)
            let ok = await store.renameDevice("d1", to: name)
            XCTAssertFalse(ok)
            XCTAssertEqual(store.deviceError, DeviceRename.lengthMessage)
            XCTAssertFalse(core.calls.contains { $0.hasPrefix("renameDevice(") })
        }
    }

    func testSixtyFourUnitsOfEmojiAreAllowed() async {
        let core = FakeCore()
        let store = await loadedStore(core)
        let name = String(repeating: "😀", count: 32)
        let ok = await store.renameDevice("d1", to: name)
        XCTAssertTrue(ok)
        XCTAssertEqual(core.calls.last, "renameDevice(d1,\(name))")
    }

    func testHubFailuresGetFriendlyText() async {
        let cases: [(String, String)] = [
            (#"hub error (404): {"error":"Device not found","code":"DEVICE_NOT_FOUND"}"#, DeviceRename.notFoundMessage),
            (#"hub error (503): {"code":"DEVICE_NAMES_UNAVAILABLE"}"#, DeviceRename.unavailableMessage),
            ("device name must be 1-64 characters", DeviceRename.lengthMessage),
            ("hub unreachable: retrying in 30s", DeviceRename.failedMessage),
        ]
        for (coreText, shown) in cases {
            let core = FakeCore()
            let store = await loadedStore(core)
            core.renameResult = .failure(CoreError(message: coreText))
            let ok = await store.renameDevice("d1", to: "Work")
            XCTAssertFalse(ok, coreText)
            XCTAssertEqual(store.deviceError, shown, coreText)
            XCTAssertEqual(store.devices.map(\.friendlyName), ["iOS", "iOS"], coreText)
        }
    }

    func testANewAttemptClearsTheOldError() async {
        let core = FakeCore()
        let store = await loadedStore(core)
        _ = await store.renameDevice("d1", to: "")
        XCTAssertNotNil(store.deviceError)
        _ = await store.renameDevice("d1", to: "Work")
        XCTAssertNil(store.deviceError)
    }
}
