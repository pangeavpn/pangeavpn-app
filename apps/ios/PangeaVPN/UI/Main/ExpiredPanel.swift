import SwiftUI
import UIKit

// No billing link or price here: App Store rule 3.1.1, so the only action is checking again.
struct ExpiredPanel: View {
    @Environment(SessionStore.self) private var session
    @Environment(ToastCenter.self) private var toasts
    @State private var checking = false

    init() {}

    private var busy: Bool { checking || session.signingOut }

    var body: some View {
        VStack(spacing: 8) {
            mark
            kicker
                .padding(.top, 4)
            Text(String(localized: "expired_title", defaultValue: "Your time has run out"))
                .scaledFont(size: 24, weight: .semibold, relativeTo: .title2)
                .tracking(-0.6)
                .foregroundStyle(Palette.text)
                .accessibilityAddTraits(.isHeader)
            Text(String(localized: "expired_body", defaultValue: "This account has no time left, so PangeaVPN can't connect. Once time is added to your account, check again."))
                .scaledFont(size: 14, relativeTo: .subheadline)
                .lineSpacing(3)
                .foregroundStyle(Palette.textSecondary)
            if let ranOut = ExpiredText.ranOut(session.subscription?.expiresAt) {
                Text(ranOut)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.textMuted)
            }
            actions
                .padding(.top, 10)
        }
        .multilineTextAlignment(.center)
        .fixedSize(horizontal: false, vertical: true)
        .frame(maxWidth: 360)
        .frame(maxWidth: .infinity)
        .padding(.vertical, 12)
    }

    private var mark: some View {
        Image(systemName: "hourglass")
            .scaledFont(size: 20, weight: .medium, relativeTo: .title3)
            .foregroundStyle(Palette.accent)
            .frame(width: 48, height: 48)
            .background(Palette.accent.opacity(0.14), in: Circle())
            .accessibilityHidden(true)
    }

    private var kicker: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(ExpiredText.warning)
                .frame(width: 7, height: 7)
                .accessibilityHidden(true)
            Text(String(localized: "subscription_none", defaultValue: "No active subscription").uppercased())
                .scaledFont(size: 11, weight: .bold, relativeTo: .caption2)
                .kerning(1.6)
                .foregroundStyle(ExpiredText.warning)
        }
    }

    private var actions: some View {
        VStack(spacing: 6) {
            PillButton(
                String(localized: "expired_check_again", defaultValue: "Check again"),
                enabled: !session.signingOut,
                loading: checking,
                action: recheck
            )
            Button(action: signOut) {
                Text(String(localized: "expired_sign_out", defaultValue: "Sign Out"))
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                    .foregroundStyle(Palette.textSecondary)
                    .frame(maxWidth: .infinity, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(PressFadeStyle())
            .disabled(busy)
            .opacity(session.signingOut ? 0.5 : 1)
        }
    }

    private func recheck() {
        guard !busy else { return }
        checking = true
        Task {
            let clock = ContinuousClock()
            let started = clock.now
            let reached = await session.refreshSubscription()
            let remaining = ExpiredText.checkHold - (clock.now - started)
            if remaining > .zero {
                try? await Task.sleep(for: remaining)
            }
            checking = false
            let verdict = ExpiredText.verdict(
                reached: reached, entitled: session.entitled, cached: session.subscription?.cached ?? false)
            toasts.show(verdict.message, style: verdict.style)
        }
    }

    private func signOut() {
        guard !busy else { return }
        Task { await session.signOut() }
    }
}

enum ExpiredVerdict: Equatable {
    case restored, stillExpired, checkFailed

    var message: String {
        switch self {
        case .restored:
            return String(localized: "expired_restored", defaultValue: "Time added — you can connect again.")
        case .stillExpired:
            return String(localized: "expired_still", defaultValue: "Still no active subscription.")
        case .checkFailed:
            return String(localized: "expired_check_failed", defaultValue: "Couldn't check right now.")
        }
    }

    var style: ToastStyle {
        switch self {
        case .restored: return .success
        case .stillExpired: return .info
        case .checkFailed: return .warning
        }
    }
}

enum ExpiredText {
    // Long enough to read as a check, so a fast answer settles instead of flashing past.
    static let checkHold = Duration.milliseconds(700)

    // Desktop's amber; light mode needs the darker shade to stay legible on cream.
    static let warning = Color(UIColor { traits in
        UIColor(hex: traits.userInterfaceStyle == .dark ? 0xF59E0B : 0xB45309)
    })

    // No answer, or a cached one, means the hub was not reached, so the old verdict proves nothing.
    static func verdict(reached: Bool, entitled: Bool, cached: Bool) -> ExpiredVerdict {
        if entitled { return .restored }
        return reached && !cached ? .stillExpired : .checkFailed
    }

    static func ranOut(_ expiresAt: String?, locale: Locale = .current, timeZone: TimeZone = .current) -> String? {
        guard let date = HubDate.medium(expiresAt, locale: locale, timeZone: timeZone) else { return nil }
        return String(format: String(localized: "expired_ran_out", defaultValue: "Ran out on %@"), date)
    }
}

#if DEBUG
@MainActor
private func expiredSession(_ expiresAt: String?) -> SessionStore {
    let core = FakeCore()
    core.restoreResult = .success(Session(email: "demo@example.org", name: "Demo", servers: []))
    core.subscriptionResult = .success(Subscription(
        status: "canceled", renews: false, expiresAt: expiresAt, entitled: false))
    return SessionStore(core: core, tunnel: FakeTunnel())
}

private struct ExpiredPreview: View {
    let session: SessionStore

    var body: some View {
        ScrollView {
            ExpiredPanel().padding(.horizontal, 20)
        }
        .background(Palette.bg)
        .environment(session)
        .environment(ToastCenter())
        .task { await session.restore() }
    }
}

#Preview("Expired") {
    ExpiredPreview(session: expiredSession("2026-10-20T00:00:00Z"))
}

#Preview("No date, dark") {
    ExpiredPreview(session: expiredSession(nil))
        .preferredColorScheme(.dark)
}
#endif
