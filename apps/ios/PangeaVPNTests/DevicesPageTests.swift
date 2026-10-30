import XCTest
@testable import PangeaVPN

private func makeDevice(_ id: String, name: String? = "iPhone", current: Bool = false) -> Device {
    Device(id: id, friendlyName: name, createdAt: "2026-01-01T00:00:00Z", status: "active", current: current)
}

final class DevicesPageTests: XCTestCase {
    private let unnamed = String(localized: "devices_unnamed", defaultValue: "Unnamed device")

    func testClampKeepsNamesWithinTheLimit() {
        XCTAssertEqual(DeviceNames.clamped(""), "")
        XCTAssertEqual(DeviceNames.clamped("Work laptop"), "Work laptop")
        let full = String(repeating: "a", count: DeviceRename.maxUnits)
        XCTAssertEqual(DeviceNames.clamped(full), full)
    }

    func testClampCutsAtTheHubLimit() {
        let long = String(repeating: "a", count: DeviceRename.maxUnits + 10)
        XCTAssertEqual(DeviceNames.clamped(long), String(repeating: "a", count: DeviceRename.maxUnits))
    }

    func testClampCountsUtf16UnitsAsTheHubDoes() {
        XCTAssertEqual(DeviceNames.clamped(String(repeating: "😀", count: 33)), String(repeating: "😀", count: 32))
        XCTAssertEqual(DeviceNames.clamped("a" + String(repeating: "😀", count: 32)), "a" + String(repeating: "😀", count: 31))
    }

    func testClampNeverSplitsACharacter() {
        let family = "👨\u{200D}👩\u{200D}👧"
        let prefix = String(repeating: "a", count: 60)
        XCTAssertEqual(DeviceNames.clamped(prefix + family), prefix)
        XCTAssertEqual(DeviceNames.clamped(String(repeating: "e\u{301}", count: 40)), String(repeating: "e\u{301}", count: 32))
    }

    func testClampedNamesAlwaysFit() {
        let inputs = ["", "x", String(repeating: "🇬🇧", count: 20), String(repeating: "ab😀", count: 30)]
        for input in inputs {
            let clamped = DeviceNames.clamped(input)
            XCTAssertLessThanOrEqual(clamped.utf16.count, DeviceRename.maxUnits, input)
            XCTAssertTrue(input.hasPrefix(clamped), input)
        }
    }

    func testDisplayShowsTheTrimmedName() {
        XCTAssertEqual(DeviceNames.display(makeDevice("d1", name: "Work laptop")), "Work laptop")
        XCTAssertEqual(DeviceNames.display(makeDevice("d1", name: "  Phone \n")), "Phone")
    }

    func testDisplayFallsBackForMissingOrBlankNames() {
        XCTAssertEqual(DeviceNames.display(makeDevice("d1", name: nil)), unnamed)
        XCTAssertEqual(DeviceNames.display(makeDevice("d1", name: "")), unnamed)
        XCTAssertEqual(DeviceNames.display(makeDevice("d1", name: " \n\t")), unnamed)
    }

    func testCurrentDeviceComesFirstAndTheRestKeepTheirOrder() {
        let list = [makeDevice("a"), makeDevice("b"), makeDevice("c", current: true), makeDevice("d")]
        XCTAssertEqual(DeviceNames.currentFirst(list).map(\.id), ["c", "a", "b", "d"])
    }

    func testOrderIsUnchangedWithoutACurrentDeviceOrWhenItIsFirst() {
        let none = [makeDevice("a"), makeDevice("b")]
        XCTAssertEqual(DeviceNames.currentFirst(none).map(\.id), ["a", "b"])
        let first = [makeDevice("a", current: true), makeDevice("b")]
        XCTAssertEqual(DeviceNames.currentFirst(first).map(\.id), ["a", "b"])
        XCTAssertEqual(DeviceNames.currentFirst([]), [])
    }
}
