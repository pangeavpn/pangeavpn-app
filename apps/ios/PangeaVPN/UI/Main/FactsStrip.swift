import SwiftUI

enum FactsLayout {
    // The widest each value gets, so the grid never reflows as the numbers grow mid-session.
    static let widestValues = ["00:00:00", "1023.9 MB", "1023.9 MB", "VLESS+REALITY"]
    static let monoAdvance: CGFloat = 0.6
    static let cellPadding: CGFloat = 14

    static func fourColumnWidth(valueSize: CGFloat) -> CGFloat {
        let characters = widestValues.reduce(0) { $0 + $1.count }
        let text = CGFloat(characters) * monoAdvance * valueSize
        let padding = CGFloat(widestValues.count) * cellPadding * 2
        let dividers = CGFloat(widestValues.count - 1)
        return text + padding + dividers
    }

    static func columns(width: CGFloat, valueSize: CGFloat) -> Int {
        width >= fourColumnWidth(valueSize: valueSize) ? 4 : 2
    }
}

struct FactsStrip: View {
    let session: String
    let down: String
    let up: String
    let via: String
    let width: CGFloat

    @ScaledMetric(relativeTo: .subheadline) private var valueSize: CGFloat = 15

    init(session: String, down: String, up: String, via: String, width: CGFloat) {
        self.session = session
        self.down = down
        self.up = up
        self.via = via
        self.width = width
    }

    private var facts: [FactItem] {
        [
            FactItem(key: String(localized: "fact_session", defaultValue: "Session"), value: session),
            FactItem(key: String(localized: "fact_down", defaultValue: "Down"), value: down),
            FactItem(key: String(localized: "fact_up", defaultValue: "Up"), value: up),
            FactItem(key: String(localized: "fact_via", defaultValue: "Via"), value: via),
        ]
    }

    private let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)

    var body: some View {
        let items = facts
        Group {
            if FactsLayout.columns(width: width, valueSize: valueSize) == 4 {
                row(items)
            } else {
                VStack(spacing: 0) {
                    row(Array(items.prefix(2)))
                    Rectangle()
                        .fill(Palette.borderSubtle)
                        .frame(height: 1)
                        .accessibilityHidden(true)
                    row(Array(items.suffix(2)))
                }
            }
        }
        .background(Palette.surface, in: shape)
        .overlay(shape.strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }

    private func row(_ cells: [FactItem]) -> some View {
        HStack(spacing: 0) {
            ForEach(Array(cells.enumerated()), id: \.offset) { index, fact in
                FactCell(fact: fact, valueSize: valueSize, leadingRule: index > 0)
            }
        }
    }
}

private struct FactItem {
    let key: String
    let value: String
}

private struct FactCell: View {
    let fact: FactItem
    let valueSize: CGFloat
    let leadingRule: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(fact.key.uppercased())
                .kerning(1.2)
                .scaledFont(size: 9.5, weight: .bold, relativeTo: .caption2)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .foregroundStyle(Palette.textSecondary)
            Text(fact.value)
                .font(.system(size: valueSize, weight: .medium, design: .monospaced))
                .lineLimit(1)
                .minimumScaleFactor(0.6)
                .foregroundStyle(Palette.text)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, FactsLayout.cellPadding)
        .padding(.vertical, 10)
        .overlay(alignment: .leading) {
            if leadingRule {
                Rectangle()
                    .fill(Palette.borderSubtle)
                    .frame(width: 1)
                    .accessibilityHidden(true)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private let dash = Format.emDash

#Preview("Phone, connected") {
    FactsStrip(session: "12:34", down: "1.5 MB", up: "320.0 KB", via: "VLESS+REALITY", width: 362)
        .padding(20)
        .background(Palette.bg)
}

#Preview("Phone, idle") {
    FactsStrip(session: dash, down: dash, up: dash, via: dash, width: 335)
        .padding(20)
        .background(Palette.bg)
}

#Preview("Wide") {
    FactsStrip(session: "1:02:03", down: "812.4 MB", up: "41.0 MB", via: "Hysteria2", width: 640)
        .padding(20)
        .frame(width: 680)
        .background(Palette.bg)
}
