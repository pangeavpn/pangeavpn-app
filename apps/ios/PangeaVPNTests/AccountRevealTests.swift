import LocalAuthentication
import XCTest
@testable import PangeaVPN

@MainActor
final class AccountRevealTests: XCTestCase {
    private let number = "ABCDEFGHJKMNPQRSTVWXYZ01"

    private func store(_ core: FakeCore, _ authenticator: FakeAuthenticator) -> SessionStore {
        SessionStore(core: core, tunnel: FakeTunnel(), authenticator: authenticator)
    }

    func testOwnerCheckComesBeforeTheNumber() async {
        let core = FakeCore()
        core.accountNumberValue = number
        let auth = FakeAuthenticator(.passed, core: core)
        let revealed = await store(core, auth).revealAccountNumber()
        XCTAssertEqual(revealed, number)
        XCTAssertEqual(core.calls, ["authenticate", "accountNumber"])
        XCTAssertEqual(auth.reasons.count, 1)
        XCTAssertFalse(auth.reasons[0].isEmpty)
    }

    func testDeniedRevealNeverReadsTheNumber() async {
        let core = FakeCore()
        core.accountNumberValue = number
        let revealed = await store(core, FakeAuthenticator(.denied, core: core)).revealAccountNumber()
        XCTAssertNil(revealed)
        XCTAssertEqual(core.calls, ["authenticate"])
    }

    func testAPhoneWithoutAPasscodeStillReveals() async {
        let core = FakeCore()
        core.accountNumberValue = number
        let revealed = await store(core, FakeAuthenticator(.noPasscode)).revealAccountNumber()
        XCTAssertEqual(revealed, number)
    }

    func testNoStoredNumberIsNil() async {
        for value in ["", "  \n"] {
            let core = FakeCore()
            core.accountNumberValue = value
            let revealed = await store(core, FakeAuthenticator(.passed)).revealAccountNumber()
            XCTAssertNil(revealed, value.debugDescription)
        }
    }

    func testOnlyAMissingPasscodeCountsAsNoOwnerCheck() {
        XCTAssertEqual(DeviceOwnerAuthenticator.outcome(failure: LAError(.passcodeNotSet) as NSError), .noPasscode)
        XCTAssertEqual(DeviceOwnerAuthenticator.outcome(failure: LAError(.passcodeNotSet)), .noPasscode)
        XCTAssertEqual(DeviceOwnerAuthenticator.outcome(failure: LAError(.userCancel)), .denied)
        XCTAssertEqual(DeviceOwnerAuthenticator.outcome(failure: LAError(.biometryLockout)), .denied)
        XCTAssertEqual(DeviceOwnerAuthenticator.outcome(failure: nil), .denied)
        let other = NSError(domain: "org.example", code: LAError.Code.passcodeNotSet.rawValue)
        XCTAssertEqual(DeviceOwnerAuthenticator.outcome(failure: other), .denied)
    }
}
