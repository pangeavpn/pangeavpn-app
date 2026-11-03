import SwiftUI

// The in-app subscription sheet: three App Store plans, the renewal terms Apple requires, and restore.
struct PaywallView: View {
    @Environment(PurchaseStore.self) private var purchases
    @Environment(SettingsStore.self) private var settings
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var selectedId: String?
    private let onClose: () -> Void

    static let termsURL = URL(string: "https://pangeavpn.org/legal/terms")!
    static let privacyURL = URL(string: "https://pangeavpn.org/legal/privacy")!
    private static let gutter: CGFloat = 20

    init(onClose: @escaping () -> Void) {
        self.onClose = onClose
    }

    private enum Phase: Equatable { case loading, failed, ready }

    private var phase: Phase {
        if !purchases.plans.isEmpty { return .ready }
        return purchases.plansFailed && !purchases.loadingPlans ? .failed : .loading
    }

    private var selected: StorePlan? {
        purchases.plans.first { $0.id == selectedId } ?? purchases.plans.first
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Spacer(minLength: 0)
                SettingsCloseButton(action: onClose)
                    .disabled(purchases.purchasing != nil)
            }
            .padding(.horizontal, 8)
            .padding(.top, 8)
            ScrollView {
                content
                    .readableWidth()
            }
            .scrollBounceBehavior(.basedOnSize)
        }
        .background(Palette.bg.ignoresSafeArea())
        .tint(Palette.accent)
        .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
        // A sheet sits above the app-wide toast and privacy layers, so it carries its own.
        .toastOverlay()
        .privacyCover()
        .interactiveDismissDisabled(purchases.purchasing != nil)
        .task { await purchases.loadPlans() }
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 0) {
            SectionLabel(String(localized: "paywall_kicker", defaultValue: "Plans"))
            Text(String(localized: "paywall_title", defaultValue: "Choose your plan"))
                .scaledFont(size: 30, weight: .light, relativeTo: .largeTitle)
                .kerning(-0.8)
                .foregroundStyle(Palette.text)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
                .padding(.top, 6)
            Text(String(localized: "paywall_subtitle", defaultValue: "PangeaVPN Premium unlocks every region and connection method on all your devices. No email or personal details needed."))
                .scaledFont(size: 14, relativeTo: .subheadline)
                .lineSpacing(2)
                .foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, 8)
            plansSection
                .padding(.top, 22)
            PillButton(
                PlanText.action(trial: selected?.trialEligible == true),
                enabled: selected != nil && !purchases.busy,
                loading: purchases.purchasing != nil,
                action: buy
            )
            .padding(.top, 18)
            FadeSwap(PlanText.terms(selected)) { terms in
                Text(terms)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .lineSpacing(2)
                    .foregroundStyle(Palette.textMuted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.top, 14)
            legalLinks
                .padding(.top, 6)
            restoreButton
        }
        .padding(.horizontal, Self.gutter)
        .padding(.top, 4)
        .padding(.bottom, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
        .animation(Motion.quick, value: phase)
    }

    @ViewBuilder
    private var plansSection: some View {
        switch phase {
        case .loading:
            ProgressView()
                .tint(Palette.accent)
                .frame(maxWidth: .infinity, minHeight: 220)
                .transition(.opacity)
        case .failed:
            VStack(alignment: .leading, spacing: 12) {
                InfoNote(String(localized: "paywall_load_failed", defaultValue: "Couldn't load plans from the App Store. Check your connection and try again."),
                         systemImage: "exclamationmark.circle", tint: Palette.error)
                GhostButton(String(localized: "restore_failed_retry", defaultValue: "Try Again")) {
                    Task { await purchases.loadPlans() }
                }
            }
            .transition(.opacity)
        case .ready:
            VStack(spacing: 10) {
                ForEach(purchases.plans) { plan in
                    PlanCard(plan: plan, selected: plan.id == selected?.id) {
                        selectedId = plan.id
                    }
                    .disabled(purchases.busy)
                }
            }
            .transition(Motion.rise(reduceMotion, by: 8))
        }
    }

    // Wraps to two lines at large text sizes rather than squeezing.
    private var legalLinks: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 18) { termsLink; privacyLink }
            VStack(alignment: .leading, spacing: 0) { termsLink; privacyLink }
        }
    }

    private var termsLink: some View {
        legalLink(String(localized: "paywall_terms_link", defaultValue: "Terms of Use"), url: Self.termsURL)
    }

    private var privacyLink: some View {
        legalLink(String(localized: "paywall_privacy_link", defaultValue: "Privacy Policy"), url: Self.privacyURL)
    }

    private func legalLink(_ title: String, url: URL) -> some View {
        Link(destination: url) {
            Text(title)
                .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                .foregroundStyle(Palette.accent)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
    }

    private var restoreButton: some View {
        Button(action: restore) {
            ZStack {
                if purchases.restoring {
                    ProgressView().tint(Palette.textSecondary)
                } else {
                    Text(String(localized: "paywall_restore", defaultValue: "Restore Purchases"))
                        .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                        .foregroundStyle(Palette.textSecondary)
                }
            }
            .frame(maxWidth: .infinity, minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(PressFadeStyle())
        .disabled(purchases.busy)
    }

    private func buy() {
        guard let plan = selected, !purchases.busy else { return }
        Task {
            if await purchases.purchase(plan) { onClose() }
        }
    }

    private func restore() {
        guard !purchases.busy else { return }
        Task {
            if await purchases.restore() != nil { onClose() }
        }
    }
}

// One plan: its length and App Store price, a check when chosen, and the trial badge when it applies.
struct PlanCard: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var typeSize
    let plan: StorePlan
    let selected: Bool
    let action: () -> Void

    private static let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(spacing: 8) {
                        Text(PlanText.title(plan.term))
                            .scaledFont(size: 14, weight: .medium, relativeTo: .subheadline)
                            .foregroundStyle(Palette.textSecondary)
                        if plan.trialEligible && !typeSize.isAccessibilitySize { badge }
                    }
                    // The billed amount must be the most prominent price on the card, ahead of the free week.
                    Text(PlanText.price(plan))
                        .scaledFont(size: 17, weight: .semibold, relativeTo: .body)
                        .foregroundStyle(Palette.text)
                        .fixedSize(horizontal: false, vertical: true)
                    if plan.trialEligible && typeSize.isAccessibilitySize { badge }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: selected ? "checkmark.circle.fill" : "circle")
                    .scaledFont(size: 22, relativeTo: .title3)
                    .foregroundStyle(selected ? Palette.accent : Palette.border)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 14)
            .background(selected ? Palette.accent.opacity(0.12) : Palette.surface, in: Self.shape)
            .overlay(Self.shape.strokeBorder(selected ? Palette.accent : Palette.borderSubtle, lineWidth: selected ? 1.5 : 1))
            .contentShape(Self.shape)
        }
        .buttonStyle(PressFadeStyle())
        .animation(Motion.state(reduceMotion), value: selected)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var badge: some View {
        Text(PlanText.trialBadge)
            .scaledFont(size: 11, weight: .semibold, relativeTo: .caption2)
            .foregroundStyle(Palette.accent)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(Palette.accent.opacity(0.12), in: Capsule())
    }
}

#if DEBUG
@MainActor
private func previewPaywall(trial: Bool) -> some View {
    let core = FakeCore()
    let client = FakeStoreClient()
    client.plansResult = .success(FakeStoreClient.samplePlans(trial: trial))
    let session = SessionStore(core: core, tunnel: FakeTunnel())
    return PaywallView(onClose: {})
        .environment(PurchaseStore(core: core, session: session, store: client))
        .environment(SettingsStore(core: core))
        .environment(ToastCenter())
}

#Preview("Free week") {
    previewPaywall(trial: true)
}

#Preview("Trial used, dark") {
    previewPaywall(trial: false)
        .preferredColorScheme(.dark)
}
#endif
