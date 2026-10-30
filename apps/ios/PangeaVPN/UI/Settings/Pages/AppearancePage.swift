import SwiftUI

// The stored darkTheme as a picker choice; nil follows the system appearance.
enum ThemeChoice: CaseIterable, Hashable, Sendable {
    case system, light, dark

    init(darkTheme: Bool?) {
        switch darkTheme {
        case .none: self = .system
        case .some(false): self = .light
        case .some(true): self = .dark
        }
    }

    var darkTheme: Bool? {
        switch self {
        case .system: return nil
        case .light: return false
        case .dark: return true
        }
    }

    var title: String {
        switch self {
        case .system: return String(localized: "appearance_theme_system", defaultValue: "System")
        case .light: return String(localized: "appearance_theme_light", defaultValue: "Light")
        case .dark: return String(localized: "appearance_theme_dark", defaultValue: "Dark")
        }
    }
}

struct AppearancePage: View {
    @Environment(SettingsStore.self) private var settings

    init() {}

    var body: some View {
        SettingsPage(.appearance, description: String(localized: "appearance_description", defaultValue: "The theme and accent colour used across the app.")) {
            SettingsCard(themeTitle) {
                Picker(themeTitle, selection: themeBinding) {
                    ForEach(ThemeChoice.allCases, id: \.self) { choice in
                        Text(choice.title).tag(choice)
                    }
                }
                .pickerStyle(.segmented)
                .frame(minHeight: 44)
            }
            SettingsCard(accentTitle) {
                AccentSwatches(selected: settings.accent, label: accentTitle) { choice in
                    settings.setAccent(choice)
                }
            }
        }
    }

    private var themeTitle: String {
        String(localized: "appearance_theme_heading", defaultValue: "Theme")
    }

    private var accentTitle: String {
        String(localized: "appearance_accent_heading", defaultValue: "Accent colour")
    }

    private var themeBinding: Binding<ThemeChoice> {
        Binding(
            get: { ThemeChoice(darkTheme: settings.darkTheme) },
            set: { settings.setDarkTheme($0.darkTheme) }
        )
    }
}

private struct AccentSwatches: View {
    let selected: Accent
    let label: String
    let choose: (Accent) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                ForEach(Accent.allCases, id: \.self) { accent in
                    swatch(accent)
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel(label)
            Text(selected.title)
                .scaledFont(size: 12, design: .monospaced, relativeTo: .caption)
                .foregroundStyle(Palette.textMuted)
                .accessibilityHidden(true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 10)
        .padding(.vertical, 10)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }

    // The ring and tick mark the choice without relying on colour alone.
    private func swatch(_ accent: Accent) -> some View {
        let isSelected = accent == selected
        return Button {
            choose(accent)
        } label: {
            ZStack {
                Circle()
                    .fill(accent.color)
                    .frame(width: 30, height: 30)
                if isSelected {
                    Image(systemName: "checkmark")
                        .font(.system(size: 13, weight: .bold))
                        .foregroundStyle(Palette.onAccent)
                    Circle()
                        .strokeBorder(accent.color, lineWidth: 2)
                        .frame(width: 40, height: 40)
                }
            }
            .frame(width: 44, height: 44)
            .contentShape(Circle())
        }
        .buttonStyle(PressFadeStyle())
        .accessibilityLabel(accent.title)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
    }
}

extension SettingsSummary {
    static func appearance(_ settings: SettingsStore) -> String {
        settings.accent.title
    }
}

#if DEBUG
#Preview("Appearance") {
    NavigationStack {
        AppearancePage()
    }
    .environment(SettingsStore(core: FakeCore()))
    .environment(ToastCenter())
}
#endif
