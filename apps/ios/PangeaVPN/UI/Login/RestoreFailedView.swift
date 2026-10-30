import SwiftUI

// A saved sign-in the hub could not confirm at launch: offer a retry, never a blank sign-in form.
struct RestoreFailedView: View {
    @Environment(SessionStore.self) private var session
    @State private var retrying = false

    init() {}

    private var busy: Bool { retrying || session.restoring || session.signingOut }

    var body: some View {
        VStack(spacing: 10) {
            BrandLogo(size: 56)
                .accessibilityHidden(true)
            Text(String(localized: "restore_failed_title", defaultValue: "Can't reach PangeaVPN"))
                .scaledFont(size: 24, weight: .semibold, relativeTo: .title2)
                .tracking(-0.6)
                .foregroundStyle(Palette.text)
                .accessibilityAddTraits(.isHeader)
                .padding(.top, 8)
            Text(String(localized: "restore_failed_body", defaultValue: "You're still signed in on this iPhone, but PangeaVPN couldn't reach its servers. Check your connection, then try again."))
                .scaledFont(size: 14, relativeTo: .subheadline)
                .lineSpacing(3)
                .foregroundStyle(Palette.textSecondary)
            PillButton(
                String(localized: "restore_failed_retry", defaultValue: "Try Again"),
                enabled: !busy,
                loading: retrying || session.restoring
            ) {
                retry()
            }
            .padding(.top, 14)
            GhostButton(String(localized: "settings_signout", defaultValue: "Sign Out")) {
                Task { await session.signOut() }
            }
            .disabled(busy)
            .opacity(busy ? 0.5 : 1)
        }
        .multilineTextAlignment(.center)
        .frame(maxWidth: 360)
        .padding(.horizontal, 24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Palette.bg.ignoresSafeArea())
    }

    private func retry() {
        guard !busy else { return }
        retrying = true
        Task {
            await session.restore()
            retrying = false
        }
    }
}

#if DEBUG
#Preview("Restore failed") {
    let core = FakeCore()
    core.restoreResult = .failure(CoreError(message: "hub unreachable: retrying in 12s"))
    let session = SessionStore(core: core, tunnel: FakeTunnel())
    return RestoreFailedView()
        .environment(session)
        .task { await session.restore() }
}
#endif
