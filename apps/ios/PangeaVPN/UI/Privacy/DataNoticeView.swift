import SwiftUI

// App Review 5.4: what the service keeps and why, shown before anyone can buy or connect.
struct DataNoticeView: View {
    enum Mode { case firstLaunch, review }

    private let mode: Mode
    private let onDone: () -> Void
    private static let gutter: CGFloat = 20

    init(mode: Mode, onDone: @escaping () -> Void) {
        self.mode = mode
        self.onDone = onDone
    }

    var body: some View {
        VStack(spacing: 0) {
            if mode == .review {
                HStack {
                    Spacer(minLength: 0)
                    SettingsCloseButton(action: onDone)
                }
                .padding(.horizontal, 8)
                .padding(.top, 8)
            }
            ScrollView {
                content
                    .readableWidth()
            }
            .scrollBounceBehavior(.basedOnSize)
            if mode == .firstLaunch {
                PillButton(String(localized: "data_notice_continue", defaultValue: "Continue"), action: onDone)
                    .padding(.horizontal, Self.gutter)
                    .readableWidth()
                    .padding(.top, 10)
                    .padding(.bottom, 16)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Palette.bg.ignoresSafeArea())
    }

    private var content: some View {
        VStack(alignment: .leading, spacing: 0) {
            SectionLabel(String(localized: "data_notice_kicker", defaultValue: "Before you start"))
            Text(String(localized: "data_notice_title", defaultValue: "Your privacy"))
                .scaledFont(size: 30, weight: .light, relativeTo: .largeTitle)
                .kerning(-0.8)
                .foregroundStyle(Palette.text)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
                .padding(.top, 6)
            Text(String(localized: "data_notice_intro", defaultValue: "PangeaVPN is built to know as little about you as possible. This is everything the service keeps, and why."))
                .scaledFont(size: 14, relativeTo: .subheadline)
                .lineSpacing(2)
                .foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.top, 8)
            VStack(alignment: .leading, spacing: 20) {
                DataNoticeRow(
                    systemImage: "tray",
                    title: String(localized: "data_notice_keep_title", defaultValue: "What we keep"),
                    detail: String(localized: "data_notice_keep_body", defaultValue: "Your account number, stored encrypted. For each device, its public key, tunnel address and the name you give it, so our servers can accept its connection. Your plan's dates, plus Apple's transaction ID if you subscribe in the app, so we know your plan is active."))
                DataNoticeRow(
                    systemImage: "eye.slash",
                    title: String(localized: "data_notice_never_title", defaultValue: "What we never keep"),
                    detail: String(localized: "data_notice_never_body", defaultValue: "The sites you visit, your traffic, your DNS lookups, when you connect or disconnect, and your IP address."))
                DataNoticeRow(
                    systemImage: "paperplane",
                    title: String(localized: "data_notice_reports_title", defaultValue: "Support reports"),
                    detail: String(localized: "data_notice_reports_body", defaultValue: "Sent only when you tap Send Report. Account numbers, keys and IP addresses are stripped on your device first, and reports are deleted after 30 days."))
                DataNoticeRow(
                    systemImage: "hand.raised",
                    title: String(localized: "data_notice_unsold_title", defaultValue: "Never sold or shared"),
                    detail: String(localized: "data_notice_unsold_body", defaultValue: "Nothing about your use of the VPN is ever sold, used for anything but running PangeaVPN, or shared with anyone."))
            }
            .padding(.top, 24)
            Link(destination: PaywallView.privacyURL) {
                Text(String(localized: "paywall_privacy_link", defaultValue: "Privacy Policy"))
                    .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                    .foregroundStyle(Palette.accent)
                    .frame(minHeight: 44)
                    .contentShape(Rectangle())
            }
            .padding(.top, 14)
        }
        .padding(.horizontal, Self.gutter)
        .padding(.top, mode == .firstLaunch ? 40 : 4)
        .padding(.bottom, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct DataNoticeRow: View {
    let systemImage: String
    let title: String
    let detail: String

    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            Image(systemName: systemImage)
                .scaledFont(size: 16, weight: .medium, relativeTo: .body)
                .foregroundStyle(Palette.accent)
                .frame(width: 36, height: 36)
                .background(Palette.accent.opacity(0.12), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(title)
                    .scaledFont(size: 16, weight: .semibold, relativeTo: .body)
                    .foregroundStyle(Palette.text)
                    .fixedSize(horizontal: false, vertical: true)
                Text(detail)
                    .scaledFont(size: 14, relativeTo: .subheadline)
                    .lineSpacing(2)
                    .foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
#Preview("First launch") {
    DataNoticeView(mode: .firstLaunch, onDone: {})
}

#Preview("From settings, dark") {
    DataNoticeView(mode: .review, onDone: {})
        .preferredColorScheme(.dark)
}
#endif
