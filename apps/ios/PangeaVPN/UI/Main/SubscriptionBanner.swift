import SwiftUI

struct SubscriptionBanner: View {
    let subscription: Subscription?

    init(subscription: Subscription?) {
        self.subscription = subscription
    }

    // Unknown says nothing, and a lapsed plan gets the expired panel instead of a line.
    static func shouldShow(_ s: Subscription?) -> Bool {
        guard let s, s.entitled != false else { return false }
        return s.status != "active" && s.status != "trialing"
    }

    static func warns(_ s: Subscription?) -> Bool {
        s?.status == "past_due"
    }

    static func text(_ s: Subscription?) -> String {
        guard let s else { return noPlan }
        if warns(s) { return SubscriptionText.line(s).text }
        guard let when = HubDate.medium(s.expiresAt) else { return noPlan }
        return String(format: String(localized: "subscription_expires", defaultValue: "Expires %1$@"), when)
    }

    private static var noPlan: String {
        String(localized: "subscription_none", defaultValue: "No active subscription")
    }

    private var tint: Color {
        Self.warns(subscription) ? Palette.connecting : Palette.textSecondary
    }

    private let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: Self.warns(subscription) ? "exclamationmark.triangle.fill" : "clock")
                .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                .foregroundStyle(tint)
                .accessibilityHidden(true)
            Text(Self.text(subscription))
                .scaledFont(size: 13, weight: .medium, relativeTo: .footnote)
                .foregroundStyle(Palette.text)
                .lineLimit(2)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(tint.opacity(0.08), in: shape)
        .overlay(shape.strokeBorder(tint.opacity(0.35), lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

#Preview("Past due") {
    SubscriptionBanner(subscription: Subscription(status: "past_due", renews: false, expiresAt: "2026-10-01T00:00:00Z"))
        .padding(20)
        .background(Palette.bg)
}

#Preview("Cancelled, dark") {
    SubscriptionBanner(subscription: Subscription(status: "canceled", renews: false, expiresAt: "2026-12-01"))
        .padding(20)
        .background(Palette.bg)
        .preferredColorScheme(.dark)
}
