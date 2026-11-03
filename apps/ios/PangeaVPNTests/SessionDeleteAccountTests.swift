import XCTest
@testable import PangeaVPN

private let signedIn = Session(email: "a@example.org", name: "A", servers: [])
private let number = "ABCDEFGHJKMNPQRSTVWXYZ01"

@MainActor
final class SessionDeleteAccountTests: XCTestCase {
    private func trackedTunnel(_ core: FakeCore) -> FakeTunnel {
        FakeTunnel(onDisconnect: { core.note("disconnect") }, onRemove: { core.note("removeConfiguration") })
    }

    private func signedInStore(_ core: any CoreBridge, fake: FakeCore) async -> SessionStore {
        fake.loginResult = .success(signedIn)
        fake.subscriptionResult = .success(Subscription(status: "active", renews: true, expiresAt: nil))
        fake.accountNumberValue = number
        let store = SessionStore(core: core, tunnel: trackedTunnel(fake))
        await store.login(number)
        return store
    }

    func testDeleteAsksTheHubFirstThenTearsDownAndSignsOut() async {
        let core = FakeCore()
        let store = await signedInStore(core, fake: core)

        let deleted = await store.deleteAccount()

        XCTAssertTrue(deleted)
        XCTAssertEqual(Array(core.calls.suffix(3)), ["deleteAccount", "disconnect", "removeConfiguration"])
        XCTAssertFalse(core.calls.contains("logout"))
        XCTAssertNil(store.session)
        XCTAssertNil(store.subscription)
        XCTAssertFalse(store.deletingAccount)
    }

    func testARefusedDeleteKeepsTheSessionAndTheTunnel() async {
        let core = FakeCore()
        let store = await signedInStore(core, fake: core)
        core.deleteAccountResult = .failure(CoreError(message: "hub unreachable: every connection method failed"))

        let deleted = await store.deleteAccount()

        XCTAssertFalse(deleted)
        XCTAssertEqual(store.session, signedIn)
        XCTAssertFalse(core.calls.contains("disconnect"))
        XCTAssertFalse(core.calls.contains("removeConfiguration"))
        XCTAssertFalse(store.deletingAccount)
    }

    func testADeleteTheHubCallsADeadSessionStillSignsOut() async {
        let core = FakeCore()
        let store = await signedInStore(core, fake: core)
        core.deleteAccountResult = .failure(CoreError(message: "hub auth error (401): INVALID_LICENSE"))
        core.accountNumberValue = ""

        let deleted = await store.deleteAccount()

        XCTAssertFalse(deleted)
        XCTAssertNil(store.session)
        XCTAssertTrue(core.calls.contains("removeConfiguration"))
    }

    func testASecondDeleteWhileOneIsInFlightIsIgnored() async {
        let fake = FakeCore()
        let core = GatedCore(fake)
        let store = await signedInStore(core, fake: fake)

        let first = Task { await store.deleteAccount() }
        let started = await eventually { store.deletingAccount }
        XCTAssertTrue(started)
        let second = await store.deleteAccount()
        await store.signOut()
        core.release()
        let firstDeleted = await first.value

        XCTAssertFalse(second)
        XCTAssertTrue(firstDeleted)
        XCTAssertEqual(fake.calls.filter { $0 == "deleteAccount" }.count, 1)
        XCTAssertFalse(fake.calls.contains("logout"))
        XCTAssertNil(store.session)
    }
}
