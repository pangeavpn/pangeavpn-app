import SwiftUI
import UIKit

// The app's languages by their own names, as desktop's language menu lists them.
enum LanguageNames {
    struct Language: Identifiable, Equatable, Sendable {
        let code: String
        let name: String
        var id: String { code }
    }

    static let supported: [Language] = [
        Language(code: "en", name: "English"),
        Language(code: "es", name: "Español"),
        Language(code: "fr", name: "Français"),
        Language(code: "ru", name: "Русский"),
        Language(code: "uk", name: "Українська"),
        Language(code: "zh", name: "中文"),
        Language(code: "ar", name: "العربية"),
        Language(code: "fa", name: "فارسی"),
    ]

    static func languageCode(_ identifier: String) -> String {
        let first = identifier.split(whereSeparator: { $0 == "-" || $0 == "_" }).first
        return (first.map { String($0) } ?? identifier).lowercased()
    }

    // Anything the app has no strings for is shown in English, so it reads as English.
    static func language(for identifier: String?) -> Language {
        let code = identifier.map { languageCode($0) } ?? ""
        return supported.first { $0.code == code } ?? supported[0]
    }

    static func nativeName(for identifier: String?) -> String {
        language(for: identifier).name
    }

    static var current: Language {
        language(for: Bundle.main.preferredLocalizations.first)
    }
}

struct LanguagePage: View {
    @Environment(ToastCenter.self) private var toasts
    private let current = LanguageNames.current

    init() {}

    var body: some View {
        SettingsPage(.language, description: String(localized: "language_description", defaultValue: "Choose the app's display language.")) {
            SettingsCard(String(localized: "language_current_heading", defaultValue: "App language")) {
                Text(current.name)
                    .scaledFont(size: 17, weight: .semibold, relativeTo: .body)
                    .foregroundStyle(Palette.text)
                    .frame(maxWidth: .infinity, minHeight: 52, alignment: .leading)
                    .padding(.horizontal, 14)
                    .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
                PillButton(String(localized: "language_change", defaultValue: "Change Language")) {
                    openSettings()
                }
                InfoNote(String(localized: "language_change_note", defaultValue: "iOS sets the language for each app. In the page that opens, tap Language and pick one. If Language isn't there, first add the language in General › Language & Region."))
            }
            SettingsCard(String(localized: "language_supported_heading", defaultValue: "Available languages")) {
                LanguageList(current: current)
            }
        }
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else {
            showOpenFailed()
            return
        }
        Task {
            let opened = await UIApplication.shared.open(url)
            if !opened { showOpenFailed() }
        }
    }

    private func showOpenFailed() {
        toasts.show(String(localized: "language_open_failed", defaultValue: "Couldn't open Settings. Open the Settings app and choose PangeaVPN."), style: .warning)
    }
}

private struct LanguageList: View {
    let current: LanguageNames.Language

    var body: some View {
        VStack(spacing: 0) {
            ForEach(LanguageNames.supported) { language in
                if language != LanguageNames.supported.first {
                    Divider().overlay(Palette.borderSubtle)
                }
                row(language)
            }
        }
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }

    private func row(_ language: LanguageNames.Language) -> some View {
        let isCurrent = language == current
        return HStack(spacing: 12) {
            Text(language.name)
                .scaledFont(size: 15, weight: isCurrent ? .semibold : .regular, relativeTo: .subheadline)
                .foregroundStyle(Palette.text)
                .frame(maxWidth: .infinity, alignment: .leading)
            Image(systemName: "checkmark")
                .scaledFont(size: 14, weight: .bold, relativeTo: .subheadline)
                .foregroundStyle(isCurrent ? Palette.accent : Color.clear)
                .accessibilityHidden(true)
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(isCurrent ? .isSelected : [])
    }
}

extension SettingsSummary {
    static func language() -> String {
        LanguageNames.current.name
    }
}

#if DEBUG
#Preview("Language") {
    NavigationStack {
        LanguagePage()
    }
    .environment(SettingsStore(core: FakeCore()))
    .environment(ToastCenter())
}
#endif
