import Foundation

// A purchase outcome worth telling the user; the id makes a repeat of the same one show again.
struct PurchaseNotice: Equatable, Identifiable, Sendable {
    enum Kind: Equatable, Sendable {
        case subscribed, restored, nothingToRestore, pending, unverified, storeFailed
        case failed(String)
    }

    let id = UUID()
    let kind: Kind

    var text: String {
        switch kind {
        case .subscribed:
            return String(localized: "purchase_subscribed", defaultValue: "You're subscribed. Thank you!")
        case .restored:
            return String(localized: "purchase_restored", defaultValue: "Purchases restored.")
        case .nothingToRestore:
            return String(localized: "purchase_nothing_to_restore", defaultValue: "No active App Store subscription was found for this Apple ID.")
        case .pending:
            return String(localized: "purchase_pending", defaultValue: "Waiting for approval. Your plan starts once the purchase is approved.")
        case .unverified:
            return String(localized: "purchase_unverified", defaultValue: "The App Store couldn't verify this purchase. Try Restore Purchases later.")
        case .storeFailed:
            return String(localized: "purchase_store_failed", defaultValue: "The App Store couldn't complete the purchase. Please try again.")
        case .failed(let message):
            return message
        }
    }

    var style: ToastStyle {
        switch kind {
        case .subscribed, .restored: return .success
        case .nothingToRestore, .pending: return .info
        case .unverified, .storeFailed, .failed: return .warning
        }
    }
}

enum PurchaseSheet: Identifiable, Equatable, Sendable {
    case paywall
    // Formatted, shown once after a purchase or restore signs in.
    case accountNumber(String)

    var id: String {
        switch self {
        case .paywall: return "paywall"
        case .accountNumber(let number): return "account-\(number)"
        }
    }
}

// Core text carries hub bodies and addresses, so only a mapped line is shown.
enum PurchaseErrorText {
    static func message(for raw: String) -> String {
        if raw.contains("APPLE_PURCHASE_CONFLICT") { return linkedElsewhere }
        if raw.contains("APPLE_PURCHASE_INVALID") { return unverifiable }
        if raw.contains("APPLE_PURCHASE_UNREACHABLE") { return unreachable }
        if raw.contains("APPLE_PURCHASE_REJECTED") {
            return answeredStatus(in: raw) == 429 ? rateLimited : notConfirmed
        }
        if let status = hubStatus(in: raw) {
            switch status {
            case 409: return linkedElsewhere
            case 400, 422: return unverifiable
            case 429: return rateLimited
            default: return notConfirmed
            }
        }
        switch LoginErrorText.kind(of: raw) {
        case .hubUnreachable, .timeout: return unreachable
        default: return notConfirmed
        }
    }

    static func hubStatus(in raw: String) -> Int? {
        guard let match = raw.range(of: #"hub (auth )?error \([0-9]{3}\)"#, options: .regularExpression) else {
            return nil
        }
        return Int(raw[match].filter(\.isNumber))
    }

    // The hub already holds this transaction under another account, so StoreKit need not resend it.
    static func isConflict(_ raw: String) -> Bool {
        raw.contains("APPLE_PURCHASE_CONFLICT")
    }

    static func answeredStatus(in raw: String) -> Int? {
        guard let match = raw.range(of: #"hub answered [0-9]{3}"#, options: .regularExpression) else {
            return nil
        }
        return Int(raw[match].filter(\.isNumber))
    }

    static var linkedElsewhere: String {
        String(localized: "purchase_error_linked", defaultValue: "This Apple ID's subscription belongs to a different PangeaVPN account. Sign in to that account to use it.")
    }

    static var unverifiable: String {
        String(localized: "purchase_error_invalid", defaultValue: "PangeaVPN couldn't verify this purchase with Apple. Try Restore Purchases.")
    }

    static var rateLimited: String {
        String(localized: "purchase_error_rate_limited", defaultValue: "Too many attempts. Wait a minute, then try again.")
    }

    static var unreachable: String {
        String(localized: "purchase_error_unreachable", defaultValue: "Couldn't reach PangeaVPN to confirm your purchase. It's safe: try Restore Purchases once you're back online.")
    }

    static var notConfirmed: String {
        String(localized: "purchase_error_server", defaultValue: "PangeaVPN couldn't confirm your purchase yet. It's safe: try Restore Purchases in a moment.")
    }
}
