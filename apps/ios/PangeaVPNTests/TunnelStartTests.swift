import Security
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

    func testProviderErrorHidesRawCoreText() {
        let error = TunnelStart.providerError(
            CoreError(message: "no transport could establish a tunnel: cloak: dial 203.0.113.9:443"))
        XCTAssertEqual(error.domain, "org.pangeavpn.tunnel")
        XCTAssertEqual(error.code, TunnelStartError.connectFailed.rawValue)
        XCTAssertEqual(error.userInfo[NSLocalizedDescriptionKey] as? String,
                       "Could not connect. Check your connection or try another region.")
    }

    func testCoreErrorsMapToUserFacingCases() {
        let cases: [(String, TunnelStartError)] = [
            ("unknown server \"de-1\"", .noServer),
            ("mobile: unknown server \"de-1\"", .noServer),
            ("SUBSCRIPTION_EXPIRED: this account's subscription has expired", .subscriptionExpired),
            ("NO_SUBSCRIPTION: this account has no active subscription", .noSubscription),
            ("hub auth error (403): forbidden", .signedOut),
            ("hub auth error (401): DEVICE_NOT_REGISTERED", .signedOut),
            ("DEVICE_NOT_REGISTERED", .signedOut),
            ("not authenticated", .signedOut),
            ("no stored session", .signedOut),
            ("no device identity", .signedOut),
            ("no transport could establish a tunnel: reality: timeout", .connectFailed),
            ("hub unreachable: retrying in 12s", .connectFailed),
            ("", .connectFailed),
        ]
        for (message, expected) in cases {
            XCTAssertEqual(TunnelStart.userFacing(CoreError(message: message)) as? TunnelStartError, expected, message)
        }
    }

    func testUserFacingKeepsNonCoreErrors() {
        XCTAssertEqual(TunnelStart.userFacing(TunnelStartError.stopped) as? TunnelStartError, .stopped)
        let system = NSError(domain: "NEVPNErrorDomain", code: 1, userInfo: [NSLocalizedDescriptionKey: "bad"])
        let error = TunnelStart.providerError(system)
        XCTAssertEqual(error.code, TunnelStart.otherErrorCode)
        XCTAssertEqual(error.localizedDescription, "bad")
    }

    func testProviderErrorIsIdempotent() {
        let once = TunnelStart.providerError(TunnelStartError.stopped)
        XCTAssertEqual(TunnelStart.providerError(once), once)
    }

    func testIsStoppedRecognisesBothForms() {
        XCTAssertTrue(TunnelStart.isStopped(TunnelStartError.stopped))
        XCTAssertTrue(TunnelStart.isStopped(TunnelStart.providerError(TunnelStartError.stopped)))
        XCTAssertFalse(TunnelStart.isStopped(TunnelStartError.signedOut))
        XCTAssertFalse(TunnelStart.isStopped(NSError(domain: "other", code: TunnelStartError.stopped.rawValue)))
    }

    func testAllowLanFollowsTheSavedProtocol() {
        var settings = AppSettings()
        settings.allowLan = false
        XCTAssertTrue(TunnelStart.resolveAllowLan(excludeLocalNetworks: true, settings: settings))
        settings.allowLan = true
        XCTAssertFalse(TunnelStart.resolveAllowLan(excludeLocalNetworks: false, settings: settings))
        XCTAssertTrue(TunnelStart.resolveAllowLan(excludeLocalNetworks: nil, settings: settings))
    }

    func testLockedKeychainMapsToDeviceLocked() {
        XCTAssertEqual(TunnelStart.keychainError(status: errSecInteractionNotAllowed), .deviceLocked)
        XCTAssertNil(TunnelStart.keychainError(status: errSecSuccess))
        XCTAssertNil(TunnelStart.keychainError(status: errSecItemNotFound))
    }

    func testNewErrorsUseEnglishDefaults() {
        XCTAssertEqual(TunnelStartError.deviceLocked.errorDescription, "Unlock your iPhone to connect.")
        XCTAssertEqual(TunnelStartError.subscriptionExpired.errorDescription, "Your subscription has expired.")
        XCTAssertEqual(TunnelStartError.connectFailed.errorDescription,
                       "Could not connect. Check your connection or try another region.")
        XCTAssertEqual(TunnelStartError.invalidDNS.errorDescription,
                       "No usable DNS server. Check Custom DNS in Settings.")
        XCTAssertEqual(TunnelStartError.settingsTimeout.errorDescription, "Could not open the tunnel interface")
    }

    func testEveryStartErrorHasTextAndItsOwnCode() {
        let codes = TunnelStartError.allCases.map { TunnelStart.providerError($0).code }
        XCTAssertEqual(Set(codes).count, TunnelStartError.allCases.count)
        XCTAssertFalse(codes.contains(TunnelStart.otherErrorCode))
        for item in TunnelStartError.allCases {
            XCTAssertFalse((item.errorDescription ?? "").isEmpty, "\(item)")
        }
    }

    func testExistingCodesKeepTheirValues() {
        XCTAssertEqual([TunnelStartError.noServer, .signedOut, .noTunnelInterface, .reconnectFailed, .stopped]
            .map(\.rawValue), [1, 2, 3, 4, 5])
    }

    func testStartErrorCodesAreDistinct() {
        let codes = [TunnelStartError.noServer, .signedOut, .noTunnelInterface].map {
            TunnelStart.providerError($0).code
        }
        XCTAssertEqual(Set(codes + [TunnelStart.otherErrorCode]).count, 4)
    }

    func testReconnectErrorsHaveTextAndDistinctCodes() {
        let cases: [TunnelStartError] = [.noServer, .signedOut, .noTunnelInterface, .reconnectFailed, .stopped]
        for item in [TunnelStartError.reconnectFailed, .stopped] {
            XCTAssertFalse((item.errorDescription ?? "").isEmpty)
        }
        let codes = cases.map { TunnelStart.providerError($0).code }
        XCTAssertEqual(Set(codes).count, cases.count)
        XCTAssertFalse(codes.contains(TunnelStart.otherErrorCode))
    }
}
