import StoreKit
import SwiftUI

// The account's plan, how it is billed, and the App Store actions: subscribe, manage and restore.
struct SubscriptionPage: View {
    @Environment(SessionStore.self) private var session
    @Environment(PurchaseStore.self) private var purchases
    @State private var showPaywall = false
    @State private var managing = false

    init() {}

    var body: some View {
        SettingsPage(.subscription, description: String(localized: "subscription_page_description", defaultValue: "Your plan, how it's billed, and App Store purchases.")) {
            SettingsCard(String(localized: "subscription_plan_card", defaultValue: "Plan")) {
                planRow
            }
            SettingsCard(String(localized: "subscription_billing_card", defaultValue: "Billing")) {
                billing
                restoreRow
                InfoNote(String(localized: "subscription_restore_hint", defaultValue: "Restore Purchases adds an App Store subscription on this Apple ID to this account."))
            }
        }
        .task { await purchases.refreshEntitlements() }
        .sheet(isPresented: $showPaywall) {
            PaywallView(onClose: { showPaywall = false })
                .formSheetSized()
        }
        .manageSubscriptionsSheet(isPresented: $managing)
        .onChange(of: managing) { was, now in
            guard was && !now else { return }
            Task {
                await purchases.refreshEntitlements()
                await session.refreshSubscription()
            }
        }
    }

    private var planRow: some View {
        let line = SubscriptionText.line(session.subscription)
        return SettingsNavRow(systemImage: "calendar", title: line.text,
                              tint: line.warn ? Palette.connecting : nil, showsChevron: false)
    }

    @ViewBuilder
    private var billing: some View {
        if purchases.appleBilled {
            SettingsNavRow(systemImage: "apple.logo",
                           title: String(localized: "subscription_billed_apple", defaultValue: "Billed through Apple"),
                           showsChevron: false)
            Button { managing = true } label: {
                SettingsNavRow(systemImage: "slider.horizontal.3",
                               title: String(localized: "subscription_manage", defaultValue: "Manage Subscription"))
            }
            .buttonStyle(.plain)
            InfoNote(String(localized: "subscription_manage_hint", defaultValue: "Change or cancel your plan in your Apple ID settings."))
        } else if session.subscription?.entitled == true {
            // The server keeps the later end date, so an App Store plan bought now would add no time.
            InfoNote(String(localized: "subscription_already_active", defaultValue: "Your plan is already active, so there's nothing to buy here. You can subscribe in the app once it ends."))
        } else {
            PillButton(String(localized: "paywall_subscribe", defaultValue: "Subscribe"), enabled: !purchases.busy) {
                showPaywall = true
            }
        }
    }

    private var restoreRow: some View {
        Button {
            Task { await purchases.restore() }
        } label: {
            SettingsNavRow(systemImage: "arrow.clockwise",
                           title: String(localized: "paywall_restore", defaultValue: "Restore Purchases"),
                           showsChevron: false)
                .opacity(purchases.restoring ? 0.5 : 1)
                .overlay(alignment: .trailing) {
                    if purchases.restoring {
                        ProgressView()
                            .tint(Palette.accent)
                            .padding(.trailing, 14)
                    }
                }
        }
        .buttonStyle(.plain)
        .disabled(purchases.busy)
    }
}

#if DEBUG
@MainActor
private func previewSubscription(appleBilled: Bool) -> some View {
    let core = FakeCore()
    core.subscriptionResult = .success(Subscription(status: "active", renews: true, expiresAt: "2026-12-26T00:00:00Z", entitled: true))
    let client = FakeStoreClient()
    if appleBilled { client.entitlements = [client.transaction(1)] }
    let session = SessionStore(core: core, tunnel: FakeTunnel())
    return NavigationStack { SubscriptionPage() }
        .environment(session)
        .environment(PurchaseStore(core: core, session: session, store: client))
        .environment(SettingsStore(core: core))
        .environment(ToastCenter())
        .task { await session.loadSubscription() }
}

#Preview("Billed through Apple") {
    previewSubscription(appleBilled: true)
}

#Preview("Not billed by Apple, dark") {
    previewSubscription(appleBilled: false)
        .preferredColorScheme(.dark)
}
#endif
