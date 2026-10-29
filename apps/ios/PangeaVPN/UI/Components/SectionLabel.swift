import SwiftUI

struct SectionLabel: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text.uppercased())
            .font(.system(size: 10, weight: .bold))
            .tracking(1.6)
            .foregroundStyle(Palette.textMuted)
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
