import XCTest
@testable import PangeaVPN

final class TunnelStartTests: XCTestCase {
    private func settings(lastServerId: String) -> AppSettings {
        var settings = AppSettings()
        settings.lastServerId = lastServerId
        return settings
    }

    func testResolveServerPrefersOption() throws {
        let options: [String: NSObject] = ["serverId": "opt" as NSString]
        let id = try TunnelStart.resolveServerID(options: options, settings: settings(lastServerId: "last"))
        XCTAssertEqual(id, "opt")
    }

    func testResolveServerFallsBackToLast() throws {
        let last = settings(lastServerId: "last")
        XCTAssertEqual(try TunnelStart.resolveServerID(options: nil, settings: last), "last")
        XCTAssertEqual(try TunnelStart.resolveServerID(options: [:], settings: last), "last")
        let empty: [String: NSObject] = ["serverId": "" as NSString]
        XCTAssertEqual(try TunnelStart.resolveServerID(options: empty, settings: last), "last")
    }

    func testResolveServerNoneThrowsNoServer() {
        XCTAssertThrowsError(try TunnelStart.resolveServerID(options: nil, settings: AppSettings())) {
            XCTAssertEqual($0 as? TunnelStartError, .noServer)
        }
    }

    func testRestoreErrorMapsSignedOut() {
        for message in ["mobile: no stored session", "mobile: no device identity"] {
            let mapped = TunnelStart.mapRestoreError(CoreError(message: message))
            XCTAssertEqual(mapped as? TunnelStartError, .signedOut, message)
        }
    }

    func testRestoreErrorPassesOthersThrough() {
        let other = CoreError(message: "mobile: fetch servers: timeout")
        XCTAssertEqual(TunnelStart.mapRestoreError(other) as? CoreError, other)
    }

    func testStartErrorsUseEnglishDefaults() {
        XCTAssertEqual(TunnelStartError.noServer.errorDescription, "Open PangeaVPN and pick a region.")
        XCTAssertEqual(TunnelStartError.signedOut.errorDescription, "Open PangeaVPN to sign in.")
        XCTAssertEqual(TunnelStartError.noTunnelInterface.errorDescription, "Could not open the tunnel interface")
    }

    func testProviderErrorCarriesStartMessage() {
        let error = TunnelStart.providerError(TunnelStartError.signedOut)
        XCTAssertEqual(error.domain, "org.pangeavpn.tunnel")
        XCTAssertEqual(error.code, TunnelStartError.signedOut.rawValue)
        XCTAssertEqual(error.userInfo[NSLocalizedDescriptionKey] as? String, "Open PangeaVPN to sign in.")
    }

    func testProviderErrorCarriesCoreMessage() {
        let error = TunnelStart.providerError(CoreError(message: "mobile: no transport could establish a tunnel"))
        XCTAssertEqual(error.domain, "org.pangeavpn.tunnel")
        XCTAssertEqual(error.code, TunnelStart.otherErrorCode)
        XCTAssertEqual(error.userInfo[NSLocalizedDescriptionKey] as? String,
                       "mobile: no transport could establish a tunnel")
    }

    func testStartErrorCodesAreDistinct() {
        let codes = [TunnelStartError.noServer, .signedOut, .noTunnelInterface].map {
            TunnelStart.providerError($0).code
        }
        XCTAssertEqual(Set(codes + [TunnelStart.otherErrorCode]).count, 4)
    }
}
