import Foundation
import StoreKit
import os

enum AppStoreClientError: Error {
    case productUnavailable
}

// The live StoreKit 2 side of StoreClient; everything it hands out is already verified on the device.
@MainActor
final class AppStoreClient: StoreClient {
    private let ids: Set<String>
    private var products: [String: Product] = [:]

    private nonisolated static let log = Logger(subsystem: "org.pangeavpn", category: "appstore")

    init(ids: [String] = StoreProducts.ids) {
        self.ids = Set(ids)
    }

    func plans() async throws -> [StorePlan] {
        let loaded = try await Product.products(for: ids)
        products = Dictionary(loaded.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        var plans: [StorePlan] = []
        for product in loaded {
            guard let info = product.subscription, let term = Self.term(info.subscriptionPeriod) else { continue }
            let hasTrial = info.introductoryOffer?.paymentMode == .freeTrial
            let eligible = hasTrial ? await info.isEligibleForIntroOffer : false
            plans.append(StorePlan(id: product.id, displayPrice: product.displayPrice, term: term, trialEligible: eligible))
        }
        return plans
    }

    func purchase(_ productId: String) async throws -> StorePurchaseResult {
        let product = try await product(productId)
        let result: Product.PurchaseResult
        do {
            result = try await product.purchase()
        } catch StoreKitError.userCancelled {
            return .cancelled
        }
        switch result {
        case .success(let verification):
            switch Self.update(verification) {
            case .verified(let transaction): return .purchased(transaction)
            case .unverified: return .unverified
            }
        case .userCancelled:
            return .cancelled
        case .pending:
            return .pending
        @unknown default:
            return .cancelled
        }
    }

    func sync() async -> StoreSyncResult {
        do {
            try await AppStore.sync()
            return .synced
        } catch StoreKitError.userCancelled {
            return .cancelled
        } catch {
            Self.log.error("App Store sync failed (\(error.logCode, privacy: .public))")
            return .failed
        }
    }

    func currentEntitlements() async -> [StoreTransaction] {
        var found: [StoreTransaction] = []
        for await result in Transaction.currentEntitlements {
            guard case .verified(let transaction) = Self.update(result), ids.contains(transaction.productId) else {
                continue
            }
            found.append(transaction)
        }
        return found
    }

    func updates() -> AsyncStream<StoreUpdate> {
        let ids = self.ids
        return AsyncStream { continuation in
            let task = Task.detached {
                for await result in Transaction.updates {
                    let update = Self.update(result)
                    if case .verified(let transaction) = update, !ids.contains(transaction.productId) { continue }
                    continuation.yield(update)
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    private func product(_ id: String) async throws -> Product {
        if let cached = products[id] { return cached }
        guard let fetched = try await Product.products(for: [id]).first else {
            throw AppStoreClientError.productUnavailable
        }
        products[id] = fetched
        return fetched
    }

    // A refund or revocation goes to the hub like any other transaction; it trims the plan itself.
    private nonisolated static func update(_ result: VerificationResult<Transaction>) -> StoreUpdate {
        switch result {
        case .verified(let transaction):
            let signed = result.jwsRepresentation
            return .verified(StoreTransaction(id: transaction.id, originalId: transaction.originalID,
                                              productId: transaction.productID, signed: signed) {
                await transaction.finish()
            })
        case .unverified(_, let error):
            log.error("App Store transaction failed verification: \(String(describing: error), privacy: .public)")
            return .unverified
        }
    }

    private nonisolated static func term(_ period: Product.SubscriptionPeriod) -> PlanTerm? {
        let months: Int
        switch period.unit {
        case .month: months = period.value
        case .year: months = period.value * 12
        default: return nil
        }
        return PlanTerm(rawValue: months)
    }
}
