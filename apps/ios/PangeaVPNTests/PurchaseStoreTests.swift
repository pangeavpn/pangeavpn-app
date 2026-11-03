import XCTest
@testable import PangeaVPN

private let signedInSession = Session(email: "a@example.org", name: "A", servers: [])
private let active = Subscription(status: "active", renews: true, expiresAt: "2026-12-01T00:00:00Z", entitled: true)
private let newNumber = "ABCDEFGHJKMNPQRSTVWXYZ01"
private let existingNumber = "ZYXWVTSRQPNMKJHGFEDCBA98"

@MainActor
final class PurchaseStoreTests: XCTestCase {
    private struct Rig {
        let core: FakeCore
        let session: SessionStore
        let client: FakeStoreClient
        let store: PurchaseStore
    }

    // Finishes are logged into the core's calls, so a test can see they came after the hub.
    private func rig(signedIn: Bool) async -> Rig {
        let core = FakeCore()
        core.loginResult = .success(signedInSession)
        core.subscriptionResult = .success(active)
        let session = SessionStore(core: core, tunnel: FakeTunnel())
        if signedIn {
            core.accountNumberValue = existingNumber
            await session.login(existingNumber)
        }
        let client = FakeStoreClient()
        client.onFinish = { core.note("finish(\($0))") }
        // Its own defaults, so a purchase left awaiting its account in one test never leaks into another.
        let defaults = UserDefaults(suiteName: "PurchaseStoreTests-\(UUID().uuidString)")!
        return Rig(core: core, session: session, client: client,
                   store: PurchaseStore(core: core, session: session, store: client, defaults: defaults))
    }

    private func monthly() -> StorePlan {
        StorePlan(id: StoreProducts.monthly, displayPrice: "£5.49", term: .month, trialEligible: true)
    }

    private func result(_ number: String, created: Bool) -> ApplePurchaseResult {
        ApplePurchaseResult(accountNumber: number, created: created,
                            expiresAt: "2026-12-01T00:00:00Z", productId: StoreProducts.monthly)
    }

    func testPlansLoadSortedByTerm() async {
        let rig = await rig(signedIn: false)
        XCTAssertNil(rig.store.trialEligible)
        await rig.store.loadPlans()
        XCTAssertEqual(rig.store.plans.map(\.term), [.month, .quarter, .year])
        XCTAssertEqual(rig.store.trialEligible, true)
        XCTAssertFalse(rig.store.plansFailed)
    }

    func testPlansThatFailToLoadSaySo() async {
        let rig = await rig(signedIn: false)
        rig.client.plansResult = .failure(CoreError(message: "offline"))
        await rig.store.loadPlans()
        XCTAssertTrue(rig.store.plansFailed)
        XCTAssertTrue(rig.store.plans.isEmpty)
        XCTAssertFalse(rig.store.loadingPlans)
    }

    func testAUsedTrialIsNotOfferedAgain() async {
        let rig = await rig(signedIn: false)
        rig.client.plansResult = .success(FakeStoreClient.samplePlans(trial: false))
        await rig.store.loadPlans()
        XCTAssertEqual(rig.store.trialEligible, false)
    }

    func testPurchaseThatCreatesAnAccountSignsInAndShowsTheNumber() async {
        let rig = await rig(signedIn: false)
        rig.core.applePurchaseResult = .success(result(newNumber, created: true))
        rig.client.purchaseResult = .purchased(rig.client.transaction(7))
        rig.store.showPaywall()

        let done = await rig.store.purchase(monthly())

        XCTAssertTrue(done)
        let calls = rig.core.calls
        let sent = calls.firstIndex(of: "applePurchase(new)")
        let finished = calls.firstIndex(of: "finish(7)")
        let login = calls.firstIndex(of: "login(\(newNumber))")
        XCTAssertNotNil(sent)
        XCTAssertLessThan(sent ?? 0, finished ?? -1)
        XCTAssertLessThan(finished ?? 0, login ?? -1)
        XCTAssertEqual(rig.client.finished, [7])
        XCTAssertEqual(rig.session.session, signedInSession)
        XCTAssertEqual(rig.store.sheet, .accountNumber("ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"))
        XCTAssertTrue(rig.store.appleBilled)
        XCTAssertNil(rig.store.purchasing)
    }

    func testANewAccountsNumberIsShownEvenWhenSignInFails() async {
        let rig = await rig(signedIn: false)
        rig.core.applePurchaseResult = .success(result(newNumber, created: true))
        rig.core.loginResult = .failure(CoreError(message: "hub unreachable: every connection method failed"))

        let done = await rig.store.purchase(monthly())

        XCTAssertTrue(done)
        XCTAssertNil(rig.session.session)
        XCTAssertEqual(rig.store.sheet, .accountNumber("ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"))
    }

    func testPurchaseForASignedInAccountCreditsItWithoutSigningInAgain() async {
        let rig = await rig(signedIn: true)
        rig.core.applePurchaseResult = .success(result(existingNumber, created: false))
        rig.client.purchaseResult = .purchased(rig.client.transaction(8))

        let done = await rig.store.purchase(monthly())

        XCTAssertTrue(done)
        let calls = rig.core.calls
        XCTAssertTrue(calls.contains("applePurchase(\(existingNumber))"))
        XCTAssertEqual(calls.filter { $0.hasPrefix("login(") }.count, 1)
        XCTAssertEqual(Array(calls.suffix(2)), ["finish(8)", "subscription"])
        XCTAssertEqual(rig.store.notice?.kind, .subscribed)
        XCTAssertNil(rig.store.sheet)
    }

    func testCancelledPurchaseLeavesEverythingAlone() async {
        let rig = await rig(signedIn: false)
        rig.client.purchaseResult = .cancelled

        let done = await rig.store.purchase(monthly())

        XCTAssertFalse(done)
        XCTAssertEqual(rig.client.purchased, [StoreProducts.monthly])
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("applePurchase") })
        XCTAssertNil(rig.store.notice)
        XCTAssertNil(rig.store.purchasing)
    }

    func testPendingPurchaseWaitsForApproval() async {
        let rig = await rig(signedIn: false)
        rig.client.purchaseResult = .pending

        let done = await rig.store.purchase(monthly())

        XCTAssertFalse(done)
        XCTAssertEqual(rig.store.notice?.kind, .pending)
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("applePurchase") })
    }

    func testUnverifiedPurchaseIsNeverSentToTheHub() async {
        let rig = await rig(signedIn: false)
        rig.client.purchaseResult = .unverified

        let done = await rig.store.purchase(monthly())

        XCTAssertFalse(done)
        XCTAssertEqual(rig.store.notice?.kind, .unverified)
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("applePurchase") })
    }

    func testAnAppStoreErrorSaysSo() async {
        let rig = await rig(signedIn: false)
        rig.client.purchaseError = CoreError(message: "network")

        let done = await rig.store.purchase(monthly())

        XCTAssertFalse(done)
        XCTAssertEqual(rig.store.notice?.kind, .storeFailed)
    }

    func testHubFailureLeavesTheTransactionUnfinished() async {
        let rig = await rig(signedIn: false)
        rig.core.applePurchaseResult = .failure(CoreError(message: "hub error (502): upstream failed"))
        rig.client.purchaseResult = .purchased(rig.client.transaction(9))

        let done = await rig.store.purchase(monthly())

        XCTAssertFalse(done)
        XCTAssertTrue(rig.client.finished.isEmpty)
        XCTAssertNil(rig.session.session)
        XCTAssertNil(rig.store.sheet)
        XCTAssertEqual(rig.store.notice?.kind, .failed(PurchaseErrorText.notConfirmed))
        XCTAssertNil(rig.store.purchasing)
    }

    func testRestoreFindsAnAccountAndSignsIn() async {
        let rig = await rig(signedIn: false)
        rig.client.entitlements = [rig.client.transaction(11)]
        rig.core.applePurchaseResult = .success(result(existingNumber, created: false))

        let found = await rig.store.restore()

        XCTAssertEqual(found, existingNumber)
        XCTAssertEqual(rig.client.syncs, 1)
        XCTAssertTrue(rig.core.calls.contains("applePurchase(new)"))
        XCTAssertTrue(rig.core.calls.contains("login(\(existingNumber))"))
        XCTAssertEqual(rig.client.finished, [11])
        XCTAssertEqual(rig.session.session, signedInSession)
        XCTAssertEqual(rig.store.sheet, .accountNumber(AccountNumber.format(existingNumber)))
        XCTAssertFalse(rig.store.restoring)
    }

    func testRestoreWhileSignedInRefreshesThePlan() async {
        let rig = await rig(signedIn: true)
        rig.client.entitlements = [rig.client.transaction(12)]
        rig.core.applePurchaseResult = .success(result(existingNumber, created: false))

        let found = await rig.store.restore()

        XCTAssertEqual(found, existingNumber)
        XCTAssertTrue(rig.core.calls.contains("applePurchase(\(existingNumber))"))
        XCTAssertEqual(rig.store.notice?.kind, .restored)
        XCTAssertNil(rig.store.sheet)
    }

    func testRestoreWithNothingToRestoreSaysSo() async {
        let rig = await rig(signedIn: false)

        let found = await rig.store.restore()

        XCTAssertNil(found)
        XCTAssertEqual(rig.client.syncs, 1)
        XCTAssertEqual(rig.store.notice?.kind, .nothingToRestore)
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("applePurchase") })
        XCTAssertFalse(rig.store.appleBilled)
    }

    func testRestoreStopsQuietlyWhenTheAppleIDPromptIsCancelled() async {
        let rig = await rig(signedIn: false)
        rig.client.syncResult = .cancelled
        rig.client.entitlements = [rig.client.transaction(13)]

        let found = await rig.store.restore()

        XCTAssertNil(found)
        XCTAssertNil(rig.store.notice)
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("applePurchase") })
    }

    // The hub already holds it under the other account, so finishing it only stops the endless redelivery.
    func testRestoreThatBelongsToAnotherAccountExplainsWhyAndIsFinished() async {
        let rig = await rig(signedIn: true)
        rig.client.entitlements = [rig.client.transaction(14)]
        rig.core.applePurchaseResult = .failure(CoreError(message: "APPLE_PURCHASE_CONFLICT: this purchase belongs to another account"))

        let found = await rig.store.restore()

        XCTAssertNil(found)
        XCTAssertEqual(rig.client.finished, [14])
        XCTAssertEqual(rig.store.notice?.kind, .failed(PurchaseErrorText.linkedElsewhere))
    }

    func testARefusalThatIsNotAConflictStaysUnfinished() async {
        let rig = await rig(signedIn: true)
        rig.core.applePurchaseResult = .failure(CoreError(message: "APPLE_PURCHASE_INVALID: the transaction could not be verified"))
        rig.client.purchaseResult = .purchased(rig.client.transaction(15))

        let done = await rig.store.purchase(monthly())

        XCTAssertFalse(done)
        XCTAssertTrue(rig.client.finished.isEmpty)
        XCTAssertEqual(rig.store.notice?.kind, .failed(PurchaseErrorText.unverifiable))
    }

    func testRestoreSendsLaterEntitlementsToTheAccountTheFirstFound() async {
        let rig = await rig(signedIn: false)
        rig.client.entitlements = [rig.client.transaction(16), rig.client.transaction(17)]
        rig.core.applePurchaseResult = .success(result(existingNumber, created: true))

        let found = await rig.store.restore()

        XCTAssertEqual(found, existingNumber)
        let sent = rig.core.calls.filter { $0.hasPrefix("applePurchase(") }
        XCTAssertEqual(sent, ["applePurchase(new)", "applePurchase(\(existingNumber))"])
        XCTAssertEqual(rig.client.finished, [16, 17])
    }

    // The hub opened the account but its reply never arrived, so the redelivery is the only way to the number.
    func testASignedOutPurchaseWhoseReplyWasLostSignsInWhenItComesBack() async {
        let rig = await rig(signedIn: false)
        rig.core.applePurchaseResult = .failure(CoreError(message: "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: context deadline exceeded"))
        rig.client.purchaseResult = .purchased(rig.client.transaction(24))
        let done = await rig.store.purchase(monthly())
        XCTAssertFalse(done)
        XCTAssertEqual(rig.store.notice?.kind, .failed(PurchaseErrorText.unreachable))

        rig.core.applePurchaseResult = .success(result(newNumber, created: false))
        rig.store.start()
        defer { rig.store.stop() }
        rig.client.send(.verified(rig.client.transaction(24)))

        let shown = await eventually { rig.store.sheet != nil }
        XCTAssertTrue(shown)
        XCTAssertEqual(rig.store.sheet, .accountNumber(AccountNumber.format(newNumber)))
        XCTAssertEqual(rig.session.session, signedInSession)
        XCTAssertEqual(rig.client.finished, [24])
    }

    // A deleted account's subscription keeps renewing; that must not quietly open a new account.
    func testARenewalWithNoAccountOpensNothing() async {
        let rig = await rig(signedIn: false)
        rig.core.applePurchaseResult = .success(result(newNumber, created: true))
        rig.store.start()
        defer { rig.store.stop() }

        rig.client.send(.verified(rig.client.transaction(21, original: 20)))

        let finished = await eventually { rig.client.finished == [21] }
        XCTAssertTrue(finished)
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("applePurchase") })
        XCTAssertNil(rig.session.session)
        XCTAssertNil(rig.store.sheet)
    }

    func testAnAskToBuyApprovalForASignedOutPurchaseSignsIn() async {
        let rig = await rig(signedIn: false)
        rig.client.purchaseResult = .pending
        let done = await rig.store.purchase(monthly())
        XCTAssertFalse(done)
        XCTAssertEqual(rig.store.notice?.kind, .pending)

        rig.core.applePurchaseResult = .success(result(newNumber, created: true))
        rig.store.start()
        defer { rig.store.stop() }
        rig.client.send(.verified(rig.client.transaction(25)))

        let shown = await eventually { rig.store.sheet != nil }
        XCTAssertTrue(shown)
        XCTAssertEqual(rig.store.sheet, .accountNumber(AccountNumber.format(newNumber)))
        XCTAssertEqual(rig.session.session, signedInSession)
        XCTAssertEqual(rig.client.finished, [25])
    }

    // An interrupted purchase (3-D Secure, say) or an offer code finishes outside purchase(); it still opens its account.
    func testAFirstPurchaseArrivingLaterOpensItsAccount() async {
        let rig = await rig(signedIn: false)
        rig.core.applePurchaseResult = .success(result(newNumber, created: true))
        rig.store.start()
        defer { rig.store.stop() }

        rig.client.send(.verified(rig.client.transaction(27)))

        let shown = await eventually { rig.store.sheet != nil }
        XCTAssertTrue(shown)
        XCTAssertEqual(rig.store.sheet, .accountNumber(AccountNumber.format(newNumber)))
        XCTAssertEqual(rig.session.session, signedInSession)
        XCTAssertEqual(rig.client.finished, [27])
    }

    // A declined Ask to Buy leaves its flag behind; a later renewal must still not reopen an account.
    func testALeftoverAwaitingFlagCannotReopenAnAccountFromARenewal() async {
        let rig = await rig(signedIn: false)
        rig.client.purchaseResult = .pending
        _ = await rig.store.purchase(monthly())

        rig.core.applePurchaseResult = .success(result(newNumber, created: true))
        rig.store.start()
        defer { rig.store.stop() }
        rig.client.send(.verified(rig.client.transaction(29, original: 28)))

        let finished = await eventually { rig.client.finished == [29] }
        XCTAssertTrue(finished)
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("applePurchase") })
        XCTAssertNil(rig.store.sheet)
    }

    func testARenewalForAStoredAccountIsStillCredited() async {
        let rig = await rig(signedIn: true)
        rig.core.applePurchaseResult = .success(result(existingNumber, created: false))
        rig.store.start()
        defer { rig.store.stop() }

        rig.client.send(.verified(rig.client.transaction(26)))

        let finished = await eventually { rig.client.finished == [26] }
        XCTAssertTrue(finished)
        XCTAssertTrue(rig.core.calls.contains("applePurchase(\(existingNumber))"))
    }

    func testRenewalForSomeoneSignedOutLeavesThemSignedOut() async {
        let rig = await rig(signedIn: false)
        rig.core.applePurchaseResult = .success(result(existingNumber, created: false))
        rig.store.start()
        defer { rig.store.stop() }

        rig.client.send(.verified(rig.client.transaction(22)))

        let finished = await eventually { rig.client.finished == [22] }
        XCTAssertTrue(finished)
        XCTAssertNil(rig.session.session)
        XCTAssertNil(rig.store.sheet)
        XCTAssertFalse(rig.core.calls.contains { $0.hasPrefix("login(") })
    }

    func testAnUpdateTheHubMissesStaysUnfinishedAndQuiet() async {
        let rig = await rig(signedIn: true)
        rig.core.applePurchaseResult = .failure(CoreError(message: "hub unreachable: every connection method failed"))
        rig.store.start()
        defer { rig.store.stop() }

        rig.client.send(.verified(rig.client.transaction(23)))

        let sent = await eventually { rig.core.calls.contains("applePurchase(\(existingNumber))") }
        XCTAssertTrue(sent)
        try? await Task.sleep(for: .milliseconds(50))
        XCTAssertTrue(rig.client.finished.isEmpty)
        XCTAssertNil(rig.store.notice)
    }

    func testEntitlementsMarkTheAccountAsBilledByApple() async {
        let rig = await rig(signedIn: true)
        await rig.store.refreshEntitlements()
        XCTAssertFalse(rig.store.appleBilled)
        rig.client.entitlements = [rig.client.transaction(30)]
        await rig.store.refreshEntitlements()
        XCTAssertTrue(rig.store.appleBilled)
    }

    func testClosingThePaywallLeavesTheAccountSheet() async {
        let rig = await rig(signedIn: false)
        rig.store.showPaywall()
        rig.store.closePaywall()
        XCTAssertNil(rig.store.sheet)
        rig.store.sheet = .accountNumber("ABCD")
        rig.store.closePaywall()
        XCTAssertEqual(rig.store.sheet, .accountNumber("ABCD"))
        rig.store.reset()
        XCTAssertNil(rig.store.sheet)
    }
}

final class PurchaseErrorTextTests: XCTestCase {
    func testHubStatusesMapToTheirMessages() {
        XCTAssertEqual(PurchaseErrorText.message(for: "hub error (409): {\"error\":\"x\"}"), PurchaseErrorText.linkedElsewhere)
        XCTAssertEqual(PurchaseErrorText.message(for: "hub error (400): bad payload"), PurchaseErrorText.unverifiable)
        XCTAssertEqual(PurchaseErrorText.message(for: "hub error (429): slow down"), PurchaseErrorText.rateLimited)
        XCTAssertEqual(PurchaseErrorText.message(for: "hub error (502): upstream"), PurchaseErrorText.notConfirmed)
        XCTAssertEqual(PurchaseErrorText.message(for: "hub auth error (401): nope"), PurchaseErrorText.notConfirmed)
    }

    // The exact strings daemon/mobile/purchase.go returns.
    func testTheCoresPurchaseMarkersMapToTheirMessages() {
        XCTAssertEqual(PurchaseErrorText.message(for: "APPLE_PURCHASE_CONFLICT: this purchase belongs to another account"),
                       PurchaseErrorText.linkedElsewhere)
        XCTAssertEqual(PurchaseErrorText.message(for: "APPLE_PURCHASE_INVALID: not a signed App Store transaction"),
                       PurchaseErrorText.unverifiable)
        XCTAssertEqual(PurchaseErrorText.message(for: "APPLE_PURCHASE_INVALID: the transaction could not be verified"),
                       PurchaseErrorText.unverifiable)
        XCTAssertEqual(PurchaseErrorText.message(for: "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: hub answered 502 (UPSTREAM_TIMEOUT)"),
                       PurchaseErrorText.unreachable)
        XCTAssertEqual(PurchaseErrorText.message(for: "APPLE_PURCHASE_REJECTED: hub answered 429 (RATE_LIMITED)"),
                       PurchaseErrorText.rateLimited)
        XCTAssertEqual(PurchaseErrorText.message(for: "APPLE_PURCHASE_REJECTED: hub answered 403 (ROUTE_NOT_ALLOWED)"),
                       PurchaseErrorText.notConfirmed)
        XCTAssertEqual(PurchaseErrorText.message(for: "APPLE_PURCHASE_BAD_ANSWER: the hub's answer did not decode"),
                       PurchaseErrorText.notConfirmed)
        XCTAssertTrue(PurchaseErrorText.isConflict("APPLE_PURCHASE_CONFLICT: this purchase belongs to another account"))
        XCTAssertFalse(PurchaseErrorText.isConflict("hub error (409): linked"))
    }

    func testTransportFailuresSayTheHubWasUnreachable() {
        XCTAssertEqual(PurchaseErrorText.message(for: "hub unreachable: every connection method failed"),
                       PurchaseErrorText.unreachable)
        XCTAssertEqual(PurchaseErrorText.message(for: "context deadline exceeded"), PurchaseErrorText.unreachable)
    }

    func testAnythingElseNeverEchoesTheCoreText() {
        let raw = "decode hub response: 203.0.113.9 said something"
        XCTAssertEqual(PurchaseErrorText.message(for: raw), PurchaseErrorText.notConfirmed)
        XCTAssertEqual(PurchaseErrorText.hubStatus(in: "hub error (418): teapot"), 418)
        XCTAssertNil(PurchaseErrorText.hubStatus(in: "no status here (200)"))
    }
}

final class PlanTextTests: XCTestCase {
    private func plan(_ term: PlanTerm, trial: Bool) -> StorePlan {
        StorePlan(id: "p", displayPrice: "£5.49", term: term, trialEligible: trial)
    }

    func testPricesNameTheirPeriod() {
        XCTAssertEqual(PlanText.price(plan(.month, trial: false)), "£5.49 per month")
        XCTAssertEqual(PlanText.price(plan(.quarter, trial: false)), "£5.49 every 3 months")
        XCTAssertEqual(PlanText.price(plan(.year, trial: false)), "£5.49 per year")
    }

    func testTermsCarryThePriceRenewalAndTrial() {
        let trial = PlanText.terms(plan(.month, trial: true))
        XCTAssertTrue(trial.hasPrefix("Free for 1 week, then £5.49 per month"))
        XCTAssertTrue(trial.contains("Renews automatically until cancelled"))
        let paid = PlanText.terms(plan(.year, trial: false))
        XCTAssertTrue(paid.hasPrefix("£5.49 per year, billed to your Apple ID."))
        XCTAssertFalse(paid.contains("Free"))
        XCTAssertTrue(PlanText.terms(nil).hasPrefix("Renews automatically"))
    }

    func testTheButtonAndSignInLinkOnlyPromiseATrialThatIsAvailable() {
        XCTAssertEqual(PlanText.action(trial: true), "Start free week")
        XCTAssertEqual(PlanText.action(trial: false), "Subscribe")
        XCTAssertEqual(PlanText.newUserLink(trialEligible: true), "New to Pangea? Start your free week")
        XCTAssertEqual(PlanText.newUserLink(trialEligible: nil), "New to Pangea? See plans")
        XCTAssertEqual(PlanText.newUserLink(trialEligible: false), "New to Pangea? See plans")
    }
}
