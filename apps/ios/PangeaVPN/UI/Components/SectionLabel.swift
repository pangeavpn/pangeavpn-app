import SwiftUI

struct SectionLabel: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text.uppercased())
            .scaledFont(size: 10, weight: .bold, relativeTo: .caption2)
            .tracking(1.6)
            .foregroundStyle(Palette.textMuted)
            .accessibilityAddTraits(.isHeader)
    }
}

#Preview {
    VStack(alignment: .leading, spacing: 12) {
        SectionLabel("Region")
        SectionLabel("Connection protocol")
    }
    .padding(24)
    .frame(maxWidth: .infinity, alignment: .leading)
    .background(Palette.bg)
}
