import SwiftUI

enum Flag {
    static let fallback = "\u{1F310}"

    static func emoji(_ country: String) -> String {
        let code = Array(country.trimmingCharacters(in: .whitespacesAndNewlines).uppercased().unicodeScalars)
        let letters = UInt32(65)...UInt32(90)
        guard code.count == 2, letters.contains(code[0].value), letters.contains(code[1].value) else {
            return fallback
        }
        let base: UInt32 = 0x1F1E6
        let scalars = code.compactMap { Unicode.Scalar(base + $0.value - 65) }
        return String(String.UnicodeScalarView(scalars))
    }
}

struct ServerRow: View {
    let name: String
    let country: String
    let load: Int?
    let selected: Bool
    let subtitle: String?
    let action: () -> Void

    init(name: String, country: String, load: Int?, selected: Bool, subtitle: String? = nil, action: @escaping () -> Void) {
        self.name = name
        self.country = country
        self.load = load
        self.selected = selected
        self.subtitle = subtitle
        self.action = action
    }

    private let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Text(Flag.emoji(country))
                    .scaledFont(size: 24, relativeTo: .title2)
                    .accessibilityHidden(true)
                titles
                if let load { LoadIndicator(load: load) }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .frame(minHeight: 54)
            .background(selected ? Palette.accent.opacity(0.13) : Palette.surface, in: shape)
            .overlay(shape.strokeBorder(selected ? Palette.accent : Palette.borderSubtle, lineWidth: 1))
            .contentShape(shape)
        }
        .buttonStyle(PressFadeStyle())
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(name)
                .scaledFont(size: 16, weight: .semibold, relativeTo: .callout)
                .foregroundStyle(Palette.text)
                .lineLimit(1)
                .truncationMode(.tail)
            if let subtitle, !subtitle.isEmpty {
                Text(subtitle)
                    .scaledFont(size: 12, design: .monospaced, relativeTo: .caption)
                    .foregroundStyle(Palette.textSecondary)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
                    .truncationMode(.middle)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

enum LoadTone {
    static func color(forPercent load: Int) -> Color {
        let pct = min(max(load, 0), 100)
        if pct < 40 { return Palette.connected }
        if pct < 75 { return Palette.connecting }
        return Palette.error
    }
}

private struct LoadIndicator: View {
    let load: Int

    private var pct: Int { min(max(load, 0), 100) }

    private var fill: Color { LoadTone.color(forPercent: load) }

    var body: some View {
        HStack(spacing: 8) {
            Capsule()
                .fill(Palette.border)
                .frame(width: 42, height: 5)
                .overlay(alignment: .leading) {
                    Capsule().fill(fill).frame(width: 42 * CGFloat(pct) / 100, height: 5)
                }
            Text(verbatim: "\(pct)%")
                .scaledFont(size: 13, design: .monospaced, relativeTo: .footnote)
                .foregroundStyle(Palette.textMuted)
                .lineLimit(1)
                .frame(minWidth: 36, alignment: .trailing)
        }
        .fixedSize()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(format: String(localized: "regions_load", defaultValue: "Server load %@"), "\(pct)%"))
    }
}

#Preview {
    VStack(spacing: 8) {
        ServerRow(name: "London", country: "GB", load: 41, selected: true, subtitle: "uk-lon-1 · best of 2", action: {})
        ServerRow(name: "Amsterdam", country: "nl", load: 33, selected: false, subtitle: "2 servers · auto", action: {})
        ServerRow(name: "Singapore", country: "SG", load: 91, selected: false, subtitle: "sg-1", action: {})
        ServerRow(name: "Unknown", country: "", load: nil, selected: false, action: {})
    }
    .padding(16)
    .background(Palette.bg)
}
