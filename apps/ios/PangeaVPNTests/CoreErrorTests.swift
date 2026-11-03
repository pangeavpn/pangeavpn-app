import XCTest
@testable import PangeaVPN

private func goError(_ message: String) -> NSError {
    NSError(domain: "go", code: 1, userInfo: [NSLocalizedDescriptionKey: message])
}

private let signedIn = Session(email: "a@example.org", name: "A", servers: [])

final class CoreErrorTests: XCTestCase {
    func testCoreErrorStripsTheGoPrefix() {
        XCTAssertEqual(CoreError.from(goError("mobile: not authenticated")).message, "not authenticated")
    }

    func testCoreErrorKeepsMessagesWithoutThePrefix() {
        XCTAssertEqual(CoreError.from(goError("hub unreachable: mobile: x")).message, "hub unreachable: mobile: x")
    }

    func testStrippedMessagesStillMatch() {
        let restore = CoreError.from(goError("mobile: no stored session"))
        XCTAssertEqual(TunnelStart.mapRestoreError(restore) as? TunnelStartError, .signedOut)
        let login = CoreError.from(goError("mobile: register device: device limit reached"))
        XCTAssertTrue(DeviceLimit.isDeviceLimit(login.message))
    }

    func testCoreErrorNilIsUnknown() {
        XCTAssertEqual(CoreError.from(nil).message, "Unknown error")
    }

    func testEmptyLicenseReadBackUndoesTheLogin() {
        for stored in ["", "  \n"] {
            var undone = 0
            XCTAssertThrowsError(try SignInCheck.confirm(signedIn, storedLicense: stored, undo: { undone += 1 })) {
                XCTAssertEqual($0 as? CoreError, .signInNotSaved)
            }
            XCTAssertEqual(undone, 1, "stored \(stored.debugDescription)")
        }
    }

    func testStoredLicenseKeepsTheSession() throws {
        var undone = 0
        let session = try SignInCheck.confirm(signedIn, storedLicense: "lk-1", undo: { undone += 1 })
        XCTAssertEqual(session, signedIn)
        XCTAssertEqual(undone, 0)
    }
}

final class CoreJSONTests: XCTestCase {
    func testNullListIsEmpty() throws {
        let servers: [Server] = try CoreJSON.decodeList("null")
        XCTAssertEqual(servers, [])
    }

    func testPaddedNullListIsEmpty() throws {
        XCTAssertTrue(CoreJSON.isNull(" null\n"))
        let devices: [Device] = try CoreJSON.decodeList(" null\n")
        XCTAssertEqual(devices, [])
    }

    func testEmptyArrayIsEmpty() throws {
        let servers: [Server] = try CoreJSON.decodeList("[]")
        XCTAssertEqual(servers, [])
    }

    func testMalformedJSONThrowsUnexpectedResponse() {
        XCTAssertThrowsError(try CoreJSON.decodeList("[{") as [Server]) {
            XCTAssertEqual($0 as? CoreError, .unexpectedResponse)
        }
    }

    func testNullOptionalIsNilAndAValueDecodes() throws {
        let missing: Subscription? = try CoreJSON.decodeOptional("null")
        XCTAssertNil(missing)
        let found: Subscription? = try CoreJSON.decodeOptional(#"{"status":"active","renews":false,"expiresAt":null}"#)
        XCTAssertEqual(found, Subscription(status: "active", renews: false, expiresAt: nil))
    }
}
