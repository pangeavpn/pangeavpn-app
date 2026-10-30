import XCTest
@testable import PangeaVPN

final class TokenInputTests: XCTestCase {
    func testKeepsFormattedAccountNumber() {
        let value = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"
        XCTAssertEqual(TokenInput.sanitize(value), value)
    }

    func testKeepsLegacyNumericToken() {
        let value = "0123456789012345"
        XCTAssertEqual(TokenInput.sanitize(value), value)
    }

    func testDropsWhitespaceAndSymbols() {
        XCTAssertEqual(TokenInput.sanitize(" ab cd\n!@#_\t12"), "abcd12")
    }

    func testDropsNonAsciiDigitsAndLetters() {
        XCTAssertEqual(TokenInput.sanitize("١٢٣٤éAB"), "AB")
    }

    func testCapsSymbolCountButNotDashes() {
        let long = String(repeating: "A", count: 5000)
        XCTAssertEqual(TokenInput.sanitize(long).count, TokenInput.maxSymbols)
        let dashed = String(repeating: "AAAA-", count: 16)
        XCTAssertEqual(TokenInput.sanitize(dashed), dashed)
    }

    func testEmpty() {
        XCTAssertEqual(TokenInput.sanitize(""), "")
    }

    // The field sanitises, then the login screen groups; a pasted lowercase code comes out grouped.
    func testPastedCodeIsSanitisedThenGrouped() {
        let typed = TokenInput.sanitize(" abcd efgh!jkmn ")
        XCTAssertEqual(AccountNumber.format(typed), "ABCD-EFGH-JKMN")
    }
}

final class LoginErrorTextTests: XCTestCase {
    func testTokenLoginStatusesMapToAccountAndThrottleAndServer() {
        XCTAssertEqual(LoginErrorText.kind(of: #"token login failed (401): {"error":"invalid"}"#), .invalidAccount)
        XCTAssertEqual(LoginErrorText.kind(of: "token login failed (404): not found"), .invalidAccount)
        XCTAssertEqual(LoginErrorText.kind(of: "token login failed (429): slow down"), .rateLimited)
        XCTAssertEqual(LoginErrorText.kind(of: "token login failed (503): busy"), .serverError)
    }

    func testRegisterStatusesMapToRegistrationAndThrottle() {
        XCTAssertEqual(LoginErrorText.kind(of: "hub auth error (401): denied"), .registrationFailed)
        XCTAssertEqual(LoginErrorText.kind(of: "hub error (429): slow down"), .rateLimited)
        XCTAssertEqual(LoginErrorText.kind(of: "hub error (502): bad gateway"), .serverError)
    }

    func testSubscriptionMarkersMapToExpired() {
        XCTAssertEqual(
            LoginErrorText.kind(of: "SUBSCRIPTION_EXPIRED: this account's subscription has expired"),
            .subscriptionExpired)
        XCTAssertEqual(LoginErrorText.kind(of: "NO_SUBSCRIPTION: this account has no active subscription"), .subscriptionExpired)
        XCTAssertEqual(
            LoginErrorText.kind(of: #"token login failed (403): {"code":"NO_ACTIVE_SUBSCRIPTION"}"#),
            .subscriptionExpired)
    }

    func testDeviceLimitWinsOverTheAuthStatus() {
        let raw = #"hub auth error (403): {"error":"Device limit reached","code":"DEVICE_LIMIT_REACHED"}"#
        XCTAssertEqual(LoginErrorText.kind(of: raw), .deviceLimit)
    }

    func testTransportFailures() {
        XCTAssertEqual(LoginErrorText.kind(of: "hub unreachable: every connection method failed"), .hubUnreachable)
        XCTAssertEqual(LoginErrorText.kind(of: "Network unreachable"), .hubUnreachable)
        XCTAssertEqual(
            LoginErrorText.kind(of: "hub request failed: Post \"https://203.0.113.9/v2\": context deadline exceeded"),
            .timeout)
        XCTAssertEqual(LoginErrorText.kind(of: "The request timed out."), .timeout)
    }

    func testPlainInvalidAccountText() {
        XCTAssertEqual(LoginErrorText.kind(of: "Invalid account number"), .invalidAccount)
    }

    func testUnknownTextIsKeptButScrubbed() {
        let kind = LoginErrorText.kind(of: "dial 203.0.113.9:443 via hub.example.org failed")
        guard case .other(let text) = kind else { return XCTFail("expected other, got \(kind)") }
        XCTAssertFalse(text.contains("203.0.113.9"))
        XCTAssertFalse(text.contains("hub.example.org"))
        XCTAssertTrue(text.hasPrefix("dial"))
    }

    func testSanitizeDropsUrlsBodiesAndLaterLines() {
        let text = LoginErrorText.sanitize("fetch https://hub.example.org/x?k=1 broke: {\"secret\":1}\nstack")
        XCTAssertFalse(text.contains("https"))
        XCTAssertFalse(text.contains("secret"))
        XCTAssertFalse(text.contains("stack"))
    }

    func testSanitizeRedactsIpv6AndCapsLength() {
        XCTAssertFalse(LoginErrorText.sanitize("dial [2001:db8::1]:443 refused").contains("2001"))
        let long = String(repeating: "word ", count: 100)
        XCTAssertLessThanOrEqual(LoginErrorText.sanitize(long).count, LoginErrorText.maxLength + 1)
    }

    func testLocalisedAppTextPassesThrough() {
        let saved = "Could not save your sign-in on this device."
        XCTAssertEqual(LoginErrorText.message(for: saved), saved)
    }

    func testEmptyFallsBackToUnknown() {
        XCTAssertEqual(LoginErrorText.message(for: "  "), LoginErrorText.unknown)
        XCTAssertEqual(LoginErrorText.message(for: "{\"error\":\"x\"}"), LoginErrorText.unknown)
    }

    func testMappedMessagesNeverEchoCoreText() {
        let raw = "token login failed (401): {\"error\":\"invalid\"}"
        XCTAssertFalse(LoginErrorText.message(for: raw).contains("401"))
    }
}
