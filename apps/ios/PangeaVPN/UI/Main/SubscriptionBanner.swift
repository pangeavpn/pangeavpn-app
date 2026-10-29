import SwiftUI

struct SubscriptionBanner: View {
    let subscription: Subscription?

    init(subscription: Subscription?) {
        self.subscription = subscription
    }

    static func shouldShow(_ s: Subscription?) -> Bool {
        guard let s else { return true }
        return s.status != "active" && s.status != "trialing"
    }

    private var text: String {
        if let expiresAt = subscription?.expiresAt {
            return String(
                format: String(localized: "subscription_expires", defaultValue: "Expires %1$@"),
                expiresAt
            )
        }
        return String(localized: "subscription_none", defaultValue: "No active subscription")
    }

    var body: some View {
        Text(text)
            .font(.subheadline)
            .foregroundStyle(Palette.textMuted)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(14)
            .background(Palette.elevated, in: RoundedRectangle(cornerRadius: 14))
    }
}

#Preview("None") {
    SubscriptionBanner(subscription: nil)
        .padding(20)
        .background(Palette.bg)
}

#Preview("Expired, dark") {
    SubscriptionBanner(subscription: Subscription(status: "canceled", renews: false, expiresAt: "2026-10-01"))
        .padding(20)
        .background(Palette.bg)
        .preferredColorScheme(.dark)
}
