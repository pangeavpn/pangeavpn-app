import SwiftUI
import UIKit

struct LoginView: View {
    @Environment(SessionStore.self) private var session
    @Environment(SettingsStore.self) private var settings
    @Environment(PurchaseStore.self) private var purchases
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Binding private var token: String
    @State private var submitting = false
    @State private var showAccountHelp = false

    private static let maxMapSize: CGFloat = 236
    private static let gutter: CGFloat = 20

    init(token: Binding<String>) {
        self._token = token
    }

    private var canSubmit: Bool {
        !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !session.loggingIn && !submitting
    }

    private var errorMessage: String? {
        session.loginError.map(LoginErrorText.message(for:))
    }

    var body: some View {
        VStack(spacing: 0) {
            PangeaHeader(version: "v\(Self.versionName)", onToggleTheme: toggleTheme)
            GeometryReader { proxy in
                let wide = AppLayout.isWide(width: proxy.size.width, sizeClass: sizeClass)
                ScrollView {
                    form(viewport: proxy.size)
                        .readableWidth()
                        .frame(minHeight: proxy.size.height, alignment: wide ? .center : .top)
                }
                .scrollDismissesKeyboard(.interactively)
            }
        }
        .background(Palette.bg.ignoresSafeArea())
        .onChange(of: token) { _, value in
            let formatted = AccountNumber.format(value)
            if formatted != value { token = formatted }
        }
        .onChange(of: errorMessage) { _, message in
            guard let message else { return }
            UIAccessibility.post(notification: .announcement, argument: message)
        }
        .task { await purchases.loadPlans() }
    }

    private func form(viewport: CGSize) -> some View {
        let available = min(viewport.width, AppLayout.readableWidth) - Self.gutter * 2
        let mapSize = max(0, min(available, viewport.height * 0.38, Self.maxMapSize))
        return VStack(alignment: .leading, spacing: 0) {
            DriftMap(state: .disconnected)
                .frame(width: mapSize, height: mapSize)
                .frame(maxWidth: .infinity)
                .accessibilityHidden(true)
            SectionLabel(String(localized: "login_title", defaultValue: "Sign In"))
            HeroHeadline(state: .disconnected)
                .padding(.top, 6)
            HeroDetail(String(localized: "login_subtitle", defaultValue: "Login with your account code."))
                .padding(.top, 6)
            accountLink
            if showAccountHelp {
                Text(String(localized: "login_find_account_help", defaultValue: "Your account number is in Settings in any PangeaVPN app you're signed in to, and on your account page on the PangeaVPN website."))
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .lineSpacing(2)
                    .foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .transition(.opacity)
            }
            SectionLabel(String(localized: "login_account_label", defaultValue: "Account number"))
                .padding(.top, 14)
                .padding(.bottom, 9)
            TokenField(
                text: $token,
                placeholder: String(
                    localized: "login_token_placeholder", defaultValue: "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"),
                label: String(localized: "login_account_label", defaultValue: "Account number"),
                enabled: !session.loggingIn,
                isError: errorMessage != nil,
                onSubmit: submit)
            if let errorMessage {
                errorBox(errorMessage).padding(.top, 14).transition(.opacity)
            }
            PillButton(
                String(localized: "login_signin", defaultValue: "Sign In"),
                enabled: canSubmit,
                loading: session.loggingIn || submitting,
                action: submit
            )
            .padding(.top, 14)
            newUserLink
                .padding(.top, 6)
                .padding(.bottom, 18)
        }
        .padding(.horizontal, Self.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .animation(Motion.quick, value: errorMessage)
        .animation(Motion.quick, value: showAccountHelp)
    }

    // Explained in place rather than linked: the website's account pages also sell plans (App Store rule 3.1.1).
    private var accountLink: some View {
        let label = String(localized: "login_find_account", defaultValue: "Find your account number")
        return Button { showAccountHelp.toggle() } label: {
            Text("\(label)\u{00A0}\(Image(systemName: showAccountHelp ? "chevron.up" : "chevron.down"))")
                .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                .multilineTextAlignment(.leading)
                .foregroundStyle(Palette.accent)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(PressFadeStyle())
        .accessibilityLabel(label)
    }

    // The free week is promised only once StoreKit says this Apple ID can still have it.
    private var newUserLink: some View {
        Button { purchases.showPaywall() } label: {
            FadeSwap(PlanText.newUserLink(trialEligible: purchases.trialEligible)) { label in
                Text(label)
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                    .foregroundStyle(Palette.accent)
                    .multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity, minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(PressFadeStyle())
        .disabled(session.loggingIn || submitting)
    }

    private func errorBox(_ message: String) -> some View {
        Text(message)
            .scaledFont(size: 13, relativeTo: .footnote)
            .foregroundStyle(Palette.error)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Palette.error.opacity(0.1), in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12).stroke(Palette.error.opacity(0.35), lineWidth: 1))
    }

    private func submit() {
        guard canSubmit else { return }
        let raw = token
        submitting = true
        Task {
            await session.login(raw)
            submitting = false
        }
    }

    private func toggleTheme() {
        settings.setDarkTheme(colorScheme == .light)
    }

    private static var versionName: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
    }
}

enum LoginErrorKind: Equatable {
    case invalidAccount, subscriptionExpired, deviceLimit, rateLimited, serverError
    case hubUnreachable, timeout, registrationFailed
    case other(String)
}

// Core text carries hub bodies, addresses and dial details, so only a mapped or scrubbed line is shown.
enum LoginErrorText {
    static let maxLength = 160

    private static let expiredMarkers = ["SUBSCRIPTION_EXPIRED", "NO_SUBSCRIPTION", "NO_ACTIVE_SUBSCRIPTION"]
    private static let invalidMarkers = ["invalid account", "invalid token", "account not found"]
    private static let timeoutMarkers = ["timeout", "timed out", "deadline exceeded"]
    private static let unreachableMarkers = [
        "hub unreachable", "hub request failed", "connection method", "network is unreachable",
        "network unreachable", "connection refused", "connection reset", "no such host",
        "no route to host", "not connected to the internet", "network connection was lost",
        "tls handshake", "certificate",
    ]
    private static let redactions = [
        #"[a-z][a-z0-9+.-]*://[^\s"']+"#,
        #"\[[0-9a-f:.%]+\](:[0-9]+)?"#,
        #"\b[0-9]{1,3}(\.[0-9]{1,3}){3}(:[0-9]+)?\b"#,
        #"\b([a-z0-9-]+\.)+[a-z]{2,}(:[0-9]+)?\b"#,
        #"\b([0-9a-f]{0,4}:){2,7}[0-9a-f]{1,4}\b"#,
    ]

    static func message(for raw: String) -> String {
        switch kind(of: raw) {
        case .invalidAccount:
            return String(localized: "login_error_invalid_account", defaultValue: "We don't recognise that account number. Check it and try again.")
        case .subscriptionExpired:
            return String(localized: "login_error_expired", defaultValue: "This account has no time left. Once time is added to your account, sign in again.")
        case .deviceLimit:
            return String(localized: "login_error_device_limit", defaultValue: "This account already has the maximum number of devices. Remove one to continue.")
        case .rateLimited:
            return String(localized: "login_error_rate_limited", defaultValue: "Too many sign-in attempts. Wait a minute, then try again.")
        case .serverError:
            return String(localized: "login_error_server", defaultValue: "Our servers are having trouble. Please try again in a moment.")
        case .hubUnreachable:
            return String(localized: "login_error_unreachable", defaultValue: "Couldn't reach PangeaVPN. Check your internet connection and try again.")
        case .timeout:
            return String(localized: "login_error_timeout", defaultValue: "Signing in took too long. Check your connection and try again.")
        case .registrationFailed:
            return String(localized: "login_error_registration", defaultValue: "Your account is fine, but this device couldn't be registered. Please try again.")
        case .other(let text):
            return text.isEmpty ? unknown : text
        }
    }

    static var unknown: String {
        String(localized: "login_error_unknown", defaultValue: "Sign in failed. Please try again.")
    }

    // The same scrubbed classes for loads outside sign-in, so no sign-in wording leaks into them.
    static func general(for raw: String) -> String {
        switch kind(of: raw) {
        case .hubUnreachable, .timeout:
            return String(localized: "login_error_unreachable", defaultValue: "Couldn't reach PangeaVPN. Check your internet connection and try again.")
        case .other(let text) where !text.isEmpty:
            return text
        case .invalidAccount, .subscriptionExpired, .deviceLimit:
            return message(for: raw)
        default:
            return String(localized: "login_error_server", defaultValue: "Our servers are having trouble. Please try again in a moment.")
        }
    }

    // Hub answers come first: their bodies can quote words the transport markers look for.
    static func kind(of raw: String) -> LoginErrorKind {
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if DeviceLimit.isDeviceLimit(text) { return .deviceLimit }
        if expiredMarkers.contains(where: { text.contains($0) }) { return .subscriptionExpired }
        if let status = status(after: "token login failed", in: text) {
            return kind(status: status, rejected: .invalidAccount)
        }
        if let status = status(after: "hub auth error", in: text) ?? status(after: "hub error", in: text) {
            return kind(status: status, rejected: .registrationFailed)
        }
        if mentions(text, invalidMarkers) { return .invalidAccount }
        if mentions(text, timeoutMarkers) { return .timeout }
        if mentions(text, unreachableMarkers) { return .hubUnreachable }
        return .other(sanitize(text))
    }

    static func sanitize(_ raw: String) -> String {
        let firstLine = raw.split(whereSeparator: \.isNewline).first.map(String.init) ?? ""
        let withoutBody = firstLine.split(separator: "{", maxSplits: 1, omittingEmptySubsequences: false)
            .first.map(String.init) ?? ""
        let redacted = redactions.reduce(withoutBody) { text, pattern in
            text.replacingOccurrences(of: pattern, with: "\u{2026}", options: [.regularExpression, .caseInsensitive])
        }
        let trimmed = redacted.trimmingCharacters(in: CharacterSet.whitespaces.union(CharacterSet(charactersIn: ":;,")))
        guard trimmed.count > maxLength else { return trimmed }
        return String(trimmed.prefix(maxLength)) + "\u{2026}"
    }

    private static func kind(status: Int, rejected: LoginErrorKind) -> LoginErrorKind {
        if status == 429 { return .rateLimited }
        if status >= 500 { return .serverError }
        return rejected
    }

    private static func status(after prefix: String, in text: String) -> Int? {
        guard let marker = text.range(of: prefix + " (") else { return nil }
        return Int(text[marker.upperBound...].prefix(3))
    }

    private static func mentions(_ text: String, _ markers: [String]) -> Bool {
        markers.contains { text.range(of: $0, options: .caseInsensitive) != nil }
    }
}

#if DEBUG
@MainActor
private func previewStores(error: String?) -> (SessionStore, SettingsStore, PurchaseStore) {
    let core = FakeCore()
    let session = SessionStore(core: core, tunnel: FakeTunnel())
    session.loginError = error
    return (session, SettingsStore(core: core), PurchaseStore(core: core, session: session, store: FakeStoreClient()))
}

private struct LoginPreview: View {
    @State private var token = ""
    let stores: (SessionStore, SettingsStore, PurchaseStore)

    var body: some View {
        LoginView(token: $token)
            .environment(stores.0)
            .environment(stores.1)
            .environment(stores.2)
    }
}

#Preview("Empty") {
    LoginPreview(stores: previewStores(error: nil))
}

#Preview("Error") {
    LoginPreview(stores: previewStores(error: "token login failed (401): {\"error\":\"invalid\"}"))
}

#Preview("Unreachable, dark") {
    LoginPreview(stores: previewStores(error: "hub unreachable: every connection method failed"))
        .preferredColorScheme(.dark)
}
#endif
