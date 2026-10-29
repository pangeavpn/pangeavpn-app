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

    private let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                Text(Flag.emoji(country)).font(.system(size: 22))
                titles
                if let load { LoadIndicator(load: load) }
                Text("\u{2713}")
                    .fontWeight(.bold)
                    .foregroundStyle(selected ? Palette.accent : Color.clear)
                    .frame(width: 18)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 11)
            .background(selected ? Palette.accent.opacity(0.15) : Palette.surface, in: shape)
            .overlay(shape.strokeBorder(selected ? Palette.accent : Palette.borderSubtle, lineWidth: 1))
            .contentShape(shape)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(name)
                .font(.system(size: 16, weight: .semibold))
                .foregroundStyle(Palette.text)
                .lineLimit(1)
                .truncationMode(.tail)
            if let subtitle {
                Text(subtitle)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(Palette.textSecondary)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct LoadIndicator: View {
    let load: Int

    private var pct: Int { min(max(load, 0), 100) }

    private var fill: Color {
        if pct < 40 { return Palette.connected }
        if pct < 75 { return Palette.connecting }
        return Palette.error
    }

    var body: some View {
        HStack(spacing: 8) {
            Capsule()
                .fill(Palette.border)
                .frame(width: 42, height: 5)
                .overlay(alignment: .leading) {
                    Capsule().fill(fill).frame(width: 42 * CGFloat(pct) / 100, height: 5)
                }
            Text("\(pct)%")
                .font(.system(size: 13))
                .foregroundStyle(Palette.textSecondary)
                .frame(minWidth: 34, alignment: .trailing)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(pct)%")
    }
}

#Preview {
    VStack(spacing: 8) {
        ServerRow(name: "London", country: "GB", load: 22, selected: true, subtitle: "uk-lon-1", action: {})
        ServerRow(name: "Frankfurt", country: "de", load: 58, selected: false, action: {})
        ServerRow(name: "Singapore", country: "SG", load: 91, selected: false, subtitle: "sg-1", action: {})
        ServerRow(name: "Unknown", country: "", load: nil, selected: false, action: {})
    }
    .padding(16)
    .background(Palette.bg)
}
