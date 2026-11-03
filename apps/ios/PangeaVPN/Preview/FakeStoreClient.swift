#if DEBUG
import Foundation

// Scripted App Store for previews, the gallery and tests; nothing here touches StoreKit.
@MainActor
final class FakeStoreClient: StoreClient {
    var plansResult: Result<[StorePlan], CoreError> = .success(FakeStoreClient.samplePlans(trial: true))
    // nil buys whatever was asked for, as transaction 1.
    var purchaseResult: StorePurchaseResult?
    var purchaseError: CoreError?
    var syncResult = StoreSyncResult.synced
    var entitlements: [StoreTransaction] = []
    // Lets a test log finishes into another fake's call order.
    var onFinish: ((UInt64) -> Void)?

    private(set) var purchased: [String] = []
    private(set) var syncs = 0
    private(set) var finished: [UInt64] = []
    private var continuation: AsyncStream<StoreUpdate>.Continuation?

    init() {}

    // Deliberately out of term order, so a caller must sort them.
    static func samplePlans(trial: Bool) -> [StorePlan] {
        [
            StorePlan(id: StoreProducts.annual, displayPrice: "£27.99", term: .year, trialEligible: trial),
            StorePlan(id: StoreProducts.monthly, displayPrice: "£5.49", term: .month, trialEligible: trial),
            StorePlan(id: StoreProducts.quarterly, displayPrice: "£12.99", term: .quarter, trialEligible: trial),
        ]
    }

    // Without an original, the transaction is a first purchase; with one, it renews that purchase.
    func transaction(_ id: UInt64, original: UInt64? = nil, product: String = StoreProducts.monthly) -> StoreTransaction {
        StoreTransaction(id: id, originalId: original ?? id, productId: product, signed: "jws-\(id)") { [weak self] in
            await self?.markFinished(id)
        }
    }

    func send(_ update: StoreUpdate) {
        continuation?.yield(update)
    }

    func plans() async throws -> [StorePlan] {
        try plansResult.get()
    }

    func purchase(_ productId: String) async throws -> StorePurchaseResult {
        purchased.append(productId)
        if let purchaseError { throw purchaseError }
        return purchaseResult ?? .purchased(transaction(1, product: productId))
    }

    func sync() async -> StoreSyncResult {
        syncs += 1
        return syncResult
    }

    func currentEntitlements() async -> [StoreTransaction] {
        entitlements
    }

    func updates() -> AsyncStream<StoreUpdate> {
        let (stream, continuation) = AsyncStream<StoreUpdate>.makeStream()
        self.continuation = continuation
        return stream
    }

    // As in StoreKit, a bought subscription then counts as a current entitlement.
    private func markFinished(_ id: UInt64) {
        finished.append(id)
        if !entitlements.contains(where: { $0.id == id }) { entitlements.append(transaction(id)) }
        onFinish?(id)
    }
}
#endif
