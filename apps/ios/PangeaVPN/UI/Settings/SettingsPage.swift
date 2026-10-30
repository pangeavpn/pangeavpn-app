import SwiftUI

// The frame every settings detail page shares: title in the nav bar, description, scrolling cards.
struct SettingsPage<Content: View>: View {
    @Environment(SettingsStore.self) private var settings
    private let route: SettingsRoute
    private let description: String
    private let content: Content

    init(_ route: SettingsRoute, description: String, @ViewBuilder content: () -> Content) {
        self.route = route
        self.description = description
        self.content = content()
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                Text(description)
                    .scaledFont(size: 14, relativeTo: .subheadline)
                    .foregroundStyle(Palette.textMuted)
                    .fixedSize(horizontal: false, vertical: true)
                if let message = settings.error {
                    InfoNote(message, systemImage: "exclamationmark.circle", tint: Palette.error)
                }
                content
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 16)
            .padding(.top, 8)
            .padding(.bottom, 32)
        }
        .background(Palette.bg.ignoresSafeArea())
        .navigationTitle(route.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(Palette.bg, for: .navigationBar)
    }
}

// A titled group of rows on a raised surface, like desktop's settings cards.
struct SettingsCard<Content: View>: View {
    private let title: String?
    private let content: Content

    init(_ title: String? = nil, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            if let title {
                SectionLabel(title)
                    .padding(.leading, 4)
            }
            content
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// Icon, title and current value, with a chevron when the row leads to another page.
struct SettingsNavRow: View {
    private let systemImage: String
    private let title: String
    private let value: String?
    private let tint: Color?
    private let showsChevron: Bool

    init(systemImage: String, title: String, value: String? = nil, tint: Color? = nil, showsChevron: Bool = true) {
        self.systemImage = systemImage
        self.title = title
        self.value = value
        self.tint = tint
        self.showsChevron = showsChevron
    }

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: systemImage)
                .scaledFont(size: 17, weight: .regular, relativeTo: .body)
                .foregroundStyle(tint ?? Palette.textSecondary)
                .frame(width: 28, height: 28)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .scaledFont(size: 16, weight: .semibold, relativeTo: .body)
                    .foregroundStyle(tint ?? Palette.text)
                    .multilineTextAlignment(.leading)
                if let value, !value.isEmpty {
                    Text(value)
                        .scaledFont(size: 12, design: .monospaced, relativeTo: .caption)
                        .foregroundStyle(Palette.textMuted)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if showsChevron {
                Image(systemName: "chevron.right")
                    .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                    .foregroundStyle(Palette.textMuted)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(minHeight: 56)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
        .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .combine)
    }
}

// A muted explanatory line with an icon, for consequences and caveats.
struct InfoNote: View {
    private let text: String
    private let systemImage: String
    private let tint: Color

    init(_ text: String, systemImage: String = "info.circle", tint: Color = Palette.textMuted) {
        self.text = text
        self.systemImage = systemImage
        self.tint = tint
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: systemImage)
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(tint)
                .accessibilityHidden(true)
            Text(text)
                .scaledFont(size: 12, relativeTo: .caption)
                .foregroundStyle(tint)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 4)
    }
}

// The top-bar button that leaves the full-screen settings.
struct SettingsCloseButton: View {
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: "xmark")
                .scaledFont(size: 15, weight: .semibold, relativeTo: .body)
                .foregroundStyle(Palette.text)
                .frame(width: 44, height: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(String(localized: "settings_close", defaultValue: "Close"))
    }
}

#if DEBUG
#Preview("Settings scaffold") {
    NavigationStack {
        SettingsPage(.network, description: "Tunnel settings and fixes for restrictive Wi-Fi.") {
            SettingsCard("Example") {
                SettingsNavRow(systemImage: "shield", title: "Censorship Bypass", value: "Automatic (recommended)")
                SettingsNavRow(systemImage: "rectangle.portrait.and.arrow.right", title: "Sign Out", tint: Palette.error, showsChevron: false)
                InfoNote("Applies on next connect.")
            }
        }
    }
    .environment(SettingsStore(core: FakeCore()))
}
#endif
