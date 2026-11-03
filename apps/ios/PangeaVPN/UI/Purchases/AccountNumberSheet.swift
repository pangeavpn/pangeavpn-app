import SwiftUI

// Shown once after a purchase or restore signs in, since a new account's number exists nowhere else yet.
struct AccountNumberSheet: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    private let number: String
    private let onDone: () -> Void

    init(number: String, onDone: @escaping () -> Void) {
        self.number = number
        self.onDone = onDone
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                mark
                SectionLabel(String(localized: "account_sheet_kicker", defaultValue: "Account ready"))
                    .padding(.top, 18)
                Text(String(localized: "account_sheet_title", defaultValue: "Your account number"))
                    .scaledFont(size: 28, weight: .light, relativeTo: .largeTitle)
                    .kerning(-0.8)
                    .foregroundStyle(Palette.text)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.isHeader)
                    .padding(.top, 6)
                numberBox
                    .padding(.top, 18)
                Text(String(localized: "account_sheet_body", defaultValue: "Save this. It's the only way to sign in on other devices and on the website. It's also in Settings."))
                    .scaledFont(size: 14, relativeTo: .subheadline)
                    .lineSpacing(2)
                    .foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 14)
                PillButton(String(localized: "settings_done", defaultValue: "Done"), action: onDone)
                    .padding(.top, 24)
            }
            .padding(.horizontal, 20)
            .padding(.top, 28)
            .padding(.bottom, 24)
            .readableWidth()
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Palette.bg.ignoresSafeArea())
        .tint(Palette.accent)
        .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
        .toastOverlay()
        .privacyCover()
    }

    private var mark: some View {
        Image(systemName: "checkmark")
            .scaledFont(size: 20, weight: .semibold, relativeTo: .title3)
            .foregroundStyle(Palette.accent)
            .frame(width: 48, height: 48)
            .background(Palette.accent.opacity(0.14), in: Circle())
            .accessibilityHidden(true)
    }

    private var numberBox: some View {
        HStack(spacing: 8) {
            Text(verbatim: number)
                .scaledFont(size: 17, weight: .medium, design: .monospaced, relativeTo: .body)
                .foregroundStyle(Palette.text)
                .minimumScaleFactor(0.6)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
                .privacySensitive()
                .accessibilityLabel(Text(verbatim: number).speechSpellsOutCharacters())
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: copy) {
                Image(systemName: "doc.on.doc")
                    .scaledFont(size: 15, weight: .medium, relativeTo: .footnote)
                    .foregroundStyle(Palette.textSecondary)
                    .frame(width: 44, height: 44)
                    .background(Palette.elevated, in: Circle())
                    .overlay(Circle().strokeBorder(Palette.border, lineWidth: 1))
                    .contentShape(Circle())
            }
            .buttonStyle(PressFadeStyle())
            .accessibilityLabel(String(localized: "settings_account_copy", defaultValue: "Copy"))
        }
        .padding(.leading, 16)
        .padding(.trailing, 8)
        .padding(.vertical, 10)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }

    private func copy() {
        AccountClipboard.copy(number)
        toasts.show(String(localized: "settings_account_copied", defaultValue: "Copied"), style: .success)
    }
}

#if DEBUG
#Preview("New account") {
    AccountNumberSheet(number: "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01", onDone: {})
        .environment(SettingsStore(core: FakeCore()))
        .environment(ToastCenter())
}
#endif
