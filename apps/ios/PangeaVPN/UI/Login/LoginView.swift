import SwiftUI

struct LoginView: View {
    @Environment(SessionStore.self) private var session
    @Environment(SettingsStore.self) private var settings
    @Environment(\.colorScheme) private var colorScheme
    @Binding private var token: String
    @State private var submitting = false

    private static let accountURL = URL(string: "https://pangeavpn.org/account")!
    private static let maxMapSize: CGFloat = 236
    private static let gutter: CGFloat = 20

    init(token: Binding<String>) {
        self._token = token
    }


    private var canSubmit: Bool {
        !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !session.loggingIn && !submitting
    }

    var body: some View {
        VStack(spacing: 0) {
            PangeaHeader(version: "v\(Self.versionName)", onToggleTheme: toggleTheme)
            GeometryReader { proxy in
                ScrollView {
                    form(viewport: proxy.size)
                        .frame(minHeight: proxy.size.height)
                }
                .scrollDismissesKeyboard(.interactively)
            }
            footer
        }
        .background(Palette.bg.ignoresSafeArea())
        .onChange(of: token) { _, value in
            let formatted = AccountNumber.format(value)
            if formatted != value { token = formatted }
        }
    }

    private func form(viewport: CGSize) -> some View {
        let available = viewport.width - Self.gutter * 2
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
            SectionLabel(String(localized: "login_account_label", defaultValue: "Account number"))
                .padding(.top, 20)
                .padding(.bottom, 9)
            TokenField(
                text: $token,
                placeholder: String(
                    localized: "login_token_placeholder", defaultValue: "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"),
                enabled: !session.loggingIn,
                isError: session.loginError != nil,
                onSubmit: submit)
            if let message = session.loginError {
                errorBox(message).padding(.top, 14)
            }
            PillButton(
                String(localized: "login_signin", defaultValue: "Sign In"),
                enabled: canSubmit,
                loading: session.loggingIn,
                action: submit
            )
            .padding(.top, 14)
            .padding(.bottom, 12)
        }
        .padding(.horizontal, Self.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func errorBox(_ message: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(message)
                .scaledFont(size: 13, relativeTo: .footnote)
                .foregroundStyle(Palette.error)
                .fixedSize(horizontal: false, vertical: true)
            Button {
                session.showDeviceLimit = true
            } label: {
                Text(String(localized: "devicelimit_title", defaultValue: "Device Limit Reached"))
                    .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                    .underline()
                    .foregroundStyle(Palette.textSecondary)
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Palette.error.opacity(0.1), in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).stroke(Palette.error.opacity(0.35), lineWidth: 1))
    }

    private var footer: some View {
        Link(destination: Self.accountURL) {
            Text(String(localized: "login_hint", defaultValue: "Find your account number at pangeavpn.org/account"))
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(Palette.textMuted)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .padding(.horizontal, Self.gutter)
                .padding(.vertical, 14)
        }
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

#if DEBUG
@MainActor
private func previewStores(error: String?) -> (SessionStore, SettingsStore) {
    let core = FakeCore()
    let session = SessionStore(core: core, tunnel: FakeTunnel())
    session.loginError = error
    return (session, SettingsStore(core: core))
}

private struct LoginPreview: View {
    @State private var token = ""
    let stores: (SessionStore, SettingsStore)

    var body: some View {
        LoginView(token: $token)
            .environment(stores.0)
            .environment(stores.1)
    }
}

#Preview("Empty") {
    LoginPreview(stores: previewStores(error: nil))
}

#Preview("Error") {
    LoginPreview(stores: previewStores(error: "Invalid account number"))
}
#endif
