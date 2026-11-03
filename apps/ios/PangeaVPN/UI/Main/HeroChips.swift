import SwiftUI

// The small tinted pills beside the state name; a post-quantum tunnel looks like any other otherwise.
struct HeroChip: View {
    let symbol: String
    let label: String
    let spoken: String
    let tint: Color

    init(symbol: String, label: String, spoken: String, tint: Color) {
        self.symbol = symbol
        self.label = label
        self.spoken = spoken
        self.tint = tint
    }

    static var postQuantum: HeroChip {
        HeroChip(
            symbol: "atom",
            label: String(localized: "main_pq_badge", defaultValue: "PQ"),
            spoken: String(localized: "main_pq_label", defaultValue: "Post-quantum protected tunnel"),
            tint: Palette.connected)
    }

    static var split: HeroChip {
        HeroChip(
            symbol: "arrow.triangle.branch",
            label: String(localized: "main_split_badge", defaultValue: "Split"),
            spoken: String(localized: "main_split_label", defaultValue: "Some addresses skip the VPN"),
            tint: Palette.accent)
    }

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: symbol)
                .scaledFont(size: 10, weight: .semibold, relativeTo: .caption2)
            Text(label)
                .kerning(0.8)
                .scaledFont(size: 10, weight: .bold, relativeTo: .caption2)
                .lineLimit(1)
        }
        .foregroundStyle(tint)
        .padding(.leading, 6)
        .padding(.trailing, 8)
        .padding(.vertical, 3)
        .background(
            LinearGradient(colors: [tint.opacity(0.16), tint.opacity(0.04)],
                           startPoint: .topLeading, endPoint: .bottomTrailing),
            in: Capsule())
        .overlay(Capsule().strokeBorder(tint.opacity(0.38), lineWidth: 1))
        .fixedSize()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
    }
}

#Preview {
    HStack(spacing: 8) {
        HeroChip.postQuantum
        HeroChip.split
    }
    .padding(24)
    .background(Palette.bg)
}
