import SwiftUI
import UIKit
import UniformTypeIdentifiers

// Desktop's account bar: the masked account number and a read-only subscription line.
struct AccountCard: View {
    @Environment(SessionStore.self) private var session
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.scenePhase) private var scenePhase
    @State private var revealed: String?
    @State private var revealing = false
    @State private var refreshing = false
    // Fresh once per visit to settings, not again on every return from a page.
    @State private var refreshed = false
    // Bumped on every hide, so an unlock that finishes after it cannot show the number again.
    @State private var generation = 0

    static let mask = "••••-••••-••••-••••-••••-••••"
    private static let clipboardLifetime: TimeInterval = 120

    init() {}

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            numberSection
            Rectangle()
                .fill(Palette.borderSubtle)
                .frame(height: 1)
                .accessibilityHidden(true)
            subscriptionSection
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
        .task { await refreshSubscription() }
        .onDisappear { hide() }
        .onChange(of: scenePhase) { _, phase in
            // The Face ID sheet makes the scene inactive, so only an idle card hides then.
            if phase == .background || (phase == .inactive && !revealing) { hide() }
        }
    }

    private var numberSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(String(localized: "settings_account_number", defaultValue: "Account Number"))
                    .scaledFont(size: 16, weight: .semibold, relativeTo: .body)
                    .foregroundStyle(Palette.text)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.isHeader)
                Spacer(minLength: 6)
                if let revealed { copyButton(revealed) }
                showButton
            }
            numberText
        }
    }

    private var numberText: some View {
        Text(verbatim: revealed ?? Self.mask)
            .scaledFont(size: 15, weight: revealed == nil ? .regular : .medium, design: .monospaced, relativeTo: .subheadline)
            .foregroundStyle(revealed == nil ? Palette.textMuted : Palette.text)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityLabel(numberLabel)
    }

    private var numberLabel: Text {
        guard let revealed else {
            return Text(String(localized: "settings_account_hidden", defaultValue: "Hidden"))
        }
        return Text(verbatim: revealed).speechSpellsOutCharacters()
    }

    private var showButton: some View {
        Button {
            if revealed == nil { reveal() } else { hide() }
        } label: {
            ZStack {
                if revealing {
                    ProgressView()
                        .controlSize(.small)
                        .tint(Palette.textSecondary)
                } else {
                    Text(revealed == nil
                        ? String(localized: "settings_account_show", defaultValue: "Show")
                        : String(localized: "settings_account_hide", defaultValue: "Hide"))
                        .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                        .foregroundStyle(Palette.textSecondary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .background(Palette.elevated, in: Capsule())
            .overlay(Capsule().strokeBorder(Palette.border, lineWidth: 1))
            .frame(minWidth: 44, minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(PressFadeStyle())
        .disabled(revealing)
    }

    private func copyButton(_ number: String) -> some View {
        Button { copy(number) } label: {
            Image(systemName: "doc.on.doc")
                .scaledFont(size: 14, weight: .medium, relativeTo: .footnote)
                .foregroundStyle(Palette.textSecondary)
                .frame(width: 44, height: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(PressFadeStyle())
        .accessibilityLabel(String(localized: "settings_account_copy", defaultValue: "Copy"))
    }

    private var subscriptionSection: some View {
        let line = SubscriptionText.line(session.subscription)
        let loading = refreshing && session.subscription == nil
        return VStack(alignment: .leading, spacing: 4) {
            Text(String(localized: "settings_account_subscription", defaultValue: "Subscription"))
                .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                .foregroundStyle(Palette.text)
            Text(loading ? String(localized: "settings_account_loading", defaultValue: "Loading...") : line.text)
                .scaledFont(size: 13, design: .monospaced, relativeTo: .footnote)
                .foregroundStyle(line.warn && !loading ? Palette.connecting : Palette.textMuted)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    // The session store asks for the device owner before it hands the number over.
    private func reveal() {
        guard !revealing else { return }
        revealing = true
        let started = generation
        Task {
            let number = await session.revealAccountNumber()
            revealing = false
            guard started == generation, let number, !number.isEmpty else { return }
            revealed = AccountNumber.format(number)
        }
    }

    private func hide() {
        generation += 1
        revealed = nil
    }

    // Local only and short-lived, so the credential neither syncs to other devices nor lingers.
    private func copy(_ number: String) {
        let expiry = Date().addingTimeInterval(Self.clipboardLifetime)
        UIPasteboard.general.setItems(
            [[UTType.plainText.identifier: number]],
            options: [.expirationDate: expiry, .localOnly: true]
        )
        toasts.show(String(localized: "settings_account_copied", defaultValue: "Copied"), style: .success)
    }

    private func refreshSubscription() async {
        guard !refreshed else { return }
        refreshed = true
        refreshing = true
        await session.refreshSubscription()
        refreshing = false
    }
}

#if DEBUG
@MainActor
private func previewCard(_ subscription: Subscription?) -> some View {
    let core = FakeCore()
    core.subscriptionResult = .success(subscription)
    core.accountNumberValue = "ABCDEFGHJKMNPQRSTVWXYZ01"
    return AccountCard()
        .padding(16)
        .frame(maxHeight: .infinity, alignment: .top)
        .background(Palette.bg)
        .environment(SessionStore(core: core, tunnel: FakeTunnel()))
        .environment(ToastCenter())
}

#Preview("Renews") {
    previewCard(Subscription(status: "active", renews: true, expiresAt: "2026-12-26T00:00:00Z", entitled: true))
}

#Preview("Expired, dark") {
    previewCard(Subscription(status: "active", renews: false, expiresAt: "2026-10-20T00:00:00Z", entitled: false, cached: true))
        .preferredColorScheme(.dark)
}
#endif
