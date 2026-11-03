import Foundation

enum StoreProducts {
    static let monthly = "org.pangeavpn.app.premium.monthly"
    static let quarterly = "org.pangeavpn.app.premium.quarterly"
    static let annual = "org.pangeavpn.app.premium.yearly"
    static let ids = [monthly, quarterly, annual]
}

enum PlanTerm: Int, Comparable, Sendable {
    case month = 1
    case quarter = 3
    case year = 12

    static func < (lhs: PlanTerm, rhs: PlanTerm) -> Bool { lhs.rawValue < rhs.rawValue }
}

// One App Store subscription as the paywall shows it.
struct StorePlan: Identifiable, Equatable, Sendable {
    let id: String
    let displayPrice: String
    let term: PlanTerm
    // The product's free-trial offer, and this Apple ID has not used one in the group.
    let trialEligible: Bool
}

// A transaction StoreKit verified on the device; the hub checks the signed form again itself.
struct StoreTransaction: Sendable {
    let id: UInt64
    let originalId: UInt64
    let productId: String
    let signed: String
    let finish: @Sendable () async -> Void

    var isRenewal: Bool { id != originalId }
}

enum StorePurchaseResult: Sendable {
    case purchased(StoreTransaction)
    // Ask to Buy or a payment needing action; the transaction arrives later as an update.
    case pending
    case cancelled
    case unverified
}

enum StoreUpdate: Sendable {
    case verified(StoreTransaction)
    case unverified
}

enum StoreSyncResult: Equatable, Sendable {
    case synced, cancelled, failed
}

// StoreKit behind a seam, so purchase logic is tested and type-checked without it.
@MainActor
protocol StoreClient: AnyObject {
    func plans() async throws -> [StorePlan]
    func purchase(_ productId: String) async throws -> StorePurchaseResult
    func sync() async -> StoreSyncResult
    // Active subscriptions on this Apple ID, verified and not revoked.
    func currentEntitlements() async -> [StoreTransaction]
    func updates() -> AsyncStream<StoreUpdate>
}
