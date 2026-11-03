import Foundation
import Observation
import os

@MainActor
@Observable
final class PurchaseStore {
    private(set) var plans: [StorePlan] = []
    private(set) var loadingPlans = false
    private(set) var plansFailed = false
    // The product being bought, held through the hub's answer and any sign-in that follows.
    private(set) var purchasing: String?
    private(set) var restoring = false
    // This Apple ID has a live App Store subscription, which only Apple can change or cancel.
    private(set) var appleBilled = false
    private(set) var notice: PurchaseNotice?
    var sheet: PurchaseSheet?

    var busy: Bool { purchasing != nil || restoring }

    // nil until the plans load; the free week is promised only once this is true.
    var trialEligible: Bool? {
        plans.isEmpty ? nil : plans.contains { $0.trialEligible }
    }

    private let core: any CoreBridge
    private let session: SessionStore
    private let store: any StoreClient
    private let defaults: UserDefaults
    @ObservationIgnored private var listener: Task<Void, Never>?
    // A transaction can reach both a purchase and the updates stream; the hub hears it once at a time.
    @ObservationIgnored private var inFlight: Set<UInt64> = []

    private static let log = Logger(subsystem: "org.pangeavpn", category: "purchases")
    // Set while a signed-out purchase waits on the hub, so a reply lost to a timeout or a kill
    // still signs in when StoreKit redelivers the transaction.
    private static let awaitingKey = "purchases.awaitingAccountNumber"

    private enum Origin { case purchase, restore, update }

    private struct Delivery {
        let result: ApplePurchaseResult
        // nil when the hub was asked to open a new account.
        let sentAccount: String?
    }

    init(core: any CoreBridge, session: SessionStore, store: any StoreClient, defaults: UserDefaults = .standard) {
        self.core = core
        self.session = session
        self.store = store
        self.defaults = defaults
    }

    private var awaitingAccount: Bool {
        get { defaults.bool(forKey: Self.awaitingKey) }
        set { defaults.set(newValue, forKey: Self.awaitingKey) }
    }

    // Renewals, Ask to Buy approvals and interrupted purchases all arrive here, even while the app was closed.
    func start() {
        guard listener == nil else { return }
        let updates = store.updates()
        listener = Task { [weak self] in
            for await update in updates {
                guard let self else { return }
                await self.receive(update)
            }
        }
        Task { await refreshEntitlements() }
    }

    func stop() {
        listener?.cancel()
        listener = nil
    }

    func showPaywall() {
        sheet = .paywall
    }

    // Leaves an account-number sheet that replaced the paywall alone.
    func closePaywall() {
        if sheet == .paywall { sheet = nil }
    }

    func reset() {
        sheet = nil
        notice = nil
        awaitingAccount = false
    }

    func loadPlans() async {
        guard !loadingPlans else { return }
        loadingPlans = true
        plansFailed = false
        do {
            let loaded = try await store.plans()
            plans = loaded.sorted { $0.term < $1.term }
            plansFailed = plans.isEmpty
        } catch {
            Self.log.error("plans failed to load (\(error.logCode, privacy: .public))")
            plansFailed = plans.isEmpty
        }
        loadingPlans = false
    }

    func refreshEntitlements() async {
        appleBilled = !(await store.currentEntitlements()).isEmpty
    }

    // True once the hub has credited the purchase; the caller then closes the paywall.
    @discardableResult
    func purchase(_ plan: StorePlan) async -> Bool {
        guard !busy else { return false }
        purchasing = plan.id
        defer { purchasing = nil }
        let outcome: StorePurchaseResult
        do {
            outcome = try await store.purchase(plan.id)
        } catch {
            Self.log.error("App Store purchase failed (\(error.logCode, privacy: .public))")
            post(.storeFailed)
            return false
        }
        switch outcome {
        case .cancelled:
            return false
        case .pending:
            // An Ask to Buy approval comes back as an update, which must still sign this purchaser in.
            if session.session == nil { awaitingAccount = true }
            post(.pending)
            return false
        case .unverified:
            post(.unverified)
            return false
        case .purchased(let transaction):
            if session.session == nil { awaitingAccount = true }
            guard let delivery = await deliver(transaction, announce: true) else { return false }
            await settle(delivery, origin: .purchase)
            return true
        }
    }

    // The account the Apple ID's subscription belongs to, or nil when there is none to restore.
    @discardableResult
    func restore() async -> String? {
        guard !busy else { return nil }
        restoring = true
        defer { restoring = false }
        // A failed sync still leaves the device's own copy of the entitlements to send.
        if await store.sync() == .cancelled { return nil }
        let entitlements = await store.currentEntitlements()
        appleBilled = !entitlements.isEmpty
        guard !entitlements.isEmpty else {
            post(.nothingToRestore)
            return nil
        }
        // Later entitlements go to the account the first one found, so a restore never opens two.
        var found: Delivery?
        for transaction in entitlements {
            let preset = found?.result.accountNumber
            if let delivery = await deliver(transaction, announce: true, account: preset) { found = delivery }
        }
        guard let found else { return nil }
        await settle(found, origin: .restore)
        return found.result.accountNumber
    }

    private func receive(_ update: StoreUpdate) async {
        guard case .verified(let transaction) = update else {
            Self.log.error("ignored an App Store update that failed verification")
            return
        }
        // A renewal with no account here (say, after Delete Account) opens nothing: Apple tells the
        // server about renewals itself, and Restore claims it. A first purchase always opens its account.
        if transaction.isRenewal, await storedAccount() == nil {
            await transaction.finish()
            return
        }
        guard let delivery = await deliver(transaction, announce: false) else { return }
        await settle(delivery, origin: .update)
    }

    private func storedAccount() async -> String? {
        let core = self.core
        let stored = await CoreQueue.perform { core.accountNumber() }
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return stored.isEmpty ? nil : stored
    }

    // Finished only once the hub holds it, so StoreKit redelivers anything the hub never saw.
    private func deliver(_ transaction: StoreTransaction, announce: Bool, account preset: String? = nil) async -> Delivery? {
        guard inFlight.insert(transaction.id).inserted else { return nil }
        defer { inFlight.remove(transaction.id) }
        let core = self.core
        let stored = preset == nil ? await storedAccount() : nil
        let account = preset ?? stored
        let signed = transaction.signed
        do {
            let result = try await CoreQueue.run {
                try core.applePurchase(signedTransaction: signed, accountNumber: account)
            }
            await transaction.finish()
            await refreshEntitlements()
            return Delivery(result: result, sentAccount: account)
        } catch {
            let raw = error.localizedDescription
            Self.log.error("hub did not credit a purchase (\(error.logCode, privacy: .public)): \(raw, privacy: .private)")
            if PurchaseErrorText.isConflict(raw) { await transaction.finish() }
            if announce { post(.failed(PurchaseErrorText.message(for: raw))) }
            return nil
        }
    }

    private func settle(_ delivery: Delivery, origin: Origin) async {
        // Any purchase the hub has credited ends the wait, so the flag never outlives it.
        let waited = awaitingAccount
        awaitingAccount = false
        let result = delivery.result
        let number = result.accountNumber.trimmingCharacters(in: .whitespacesAndNewlines)
        guard session.session == nil else {
            await session.refreshSubscription()
            // Someone signed in while a signed-out purchase was at the hub; its new account must not vanish.
            if delivery.sentAccount == nil && result.created && !number.isEmpty {
                reveal(number)
                return
            }
            switch origin {
            case .purchase: post(.subscribed)
            case .restore: post(.restored)
            case .update: break
            }
            return
        }
        // A renewal for someone who signed out leaves them signed out, unless their own purchase's reply was lost.
        if origin == .update && !result.created && !waited { return }
        guard !number.isEmpty else { return }
        await session.login(number)
        reveal(number)
    }

    // Shown even if sign-in failed, so the only copy of a new account's number is never lost.
    private func reveal(_ number: String) {
        awaitingAccount = false
        sheet = .accountNumber(AccountNumber.format(number))
    }

    private func post(_ kind: PurchaseNotice.Kind) {
        notice = PurchaseNotice(kind: kind)
    }
}
