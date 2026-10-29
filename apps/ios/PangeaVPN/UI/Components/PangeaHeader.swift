import SwiftUI

struct PangeaHeader: View {
    let version: String
    let onToggleTheme: () -> Void
    let onMenu: (() -> Void)?

    init(version: String, onToggleTheme: @escaping () -> Void, onMenu: (() -> Void)? = nil) {
        self.version = version
        self.onToggleTheme = onToggleTheme
        self.onMenu = onMenu
    }

    var body: some View {
        HStack(spacing: 10) {
            BrandLogo()
            Text(version)
                .scaledFont(size: 11, relativeTo: .caption2)
                .foregroundStyle(Palette.textMuted)
            Spacer(minLength: 0)
            IconControl(
                symbol: "sun.max",
                label: String(localized: "header_toggle_theme", defaultValue: "Toggle theme"),
                action: onToggleTheme
            )
            if let onMenu {
                IconControl(
                    symbol: "line.3.horizontal",
                    label: String(localized: "header_menu", defaultValue: "Menu"),
                    action: onMenu
                )
            }
        }
        .padding(.horizontal, 18)
        .frame(minHeight: 56)
    }
}

private struct IconControl: View {
    let symbol: String
    let label: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .scaledFont(size: 19, weight: .regular, relativeTo: .title3)
                .foregroundStyle(Palette.textMuted)
                .frame(width: 40, height: 40)
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

#Preview {
    VStack(spacing: 0) {
        PangeaHeader(version: "v0.8.2", onToggleTheme: {}, onMenu: {})
        PangeaHeader(version: "v0.8.2", onToggleTheme: {})
    }
    .background(Palette.bg)
}
