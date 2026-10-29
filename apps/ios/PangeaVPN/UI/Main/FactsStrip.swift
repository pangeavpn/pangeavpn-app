import SwiftUI

struct FactsStrip: View {
    let session: String
    let down: String
    let up: String
    let via: String

    init(session: String, down: String, up: String, via: String) {
        self.session = session
        self.down = down
        self.up = up
        self.via = via
    }

    private var facts: [(key: String, value: String)] {
        [
            (String(localized: "fact_session", defaultValue: "Session"), session),
            (String(localized: "fact_down", defaultValue: "Down"), down),
            (String(localized: "fact_up", defaultValue: "Up"), up),
            (String(localized: "fact_via", defaultValue: "Via"), via),
        ]
    }

    var body: some View {
        let items = facts
        HStack(alignment: .top, spacing: 0) {
            ForEach(Array(items.enumerated()), id: \.offset) { index, fact in
                Fact(key: fact.key, value: fact.value)
                if index < items.count - 1 {
                    Rectangle()
                        .fill(Palette.borderSubtle)
                        .frame(width: 1, height: 30)
                        .padding(.horizontal, 10)
                        .accessibilityHidden(true)
                }
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 14)
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
    }
}

private struct Fact: View {
    let key: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(key.uppercased())
                .scaledFont(size: 9, weight: .bold, relativeTo: .caption2)
                .kerning(1.3)
                .lineLimit(1)
                .foregroundStyle(Palette.textMuted)
            Text(value)
                .scaledFont(size: 15, design: .monospaced, relativeTo: .subheadline)
                .lineLimit(1)
                .truncationMode(.tail)
                .foregroundStyle(Palette.text)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

private let dash = Format.emDash

#Preview("Disconnected") {
    FactsStrip(session: dash, down: dash, up: dash, via: dash)
        .padding()
        .background(Palette.bg)
}

#Preview("Connecting") {
    FactsStrip(session: dash, down: "0 B", up: "0 B", via: "Auto")
        .padding()
        .background(Palette.bg)
}

#Preview("Connected") {
    FactsStrip(session: "12:34", down: "1.5 MB", up: "320.0 KB", via: "Shadowsocks")
        .padding()
        .background(Palette.bg)
}

#Preview("Disconnecting") {
    FactsStrip(session: "1:02:03", down: "812.4 MB", up: "41.0 MB", via: "Hysteria2")
        .padding()
        .background(Palette.bg)
}

#Preview("Error") {
    FactsStrip(session: dash, down: dash, up: dash, via: dash)
        .padding()
        .background(Palette.bg)
}
