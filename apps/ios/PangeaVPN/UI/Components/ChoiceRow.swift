import SwiftUI

struct ChoiceRow: View {
    private let label: String
    private let selected: Bool
    private let hint: String?
    private let action: () -> Void

    init(_ label: String, selected: Bool, hint: String? = nil, action: @escaping () -> Void) {
        self.label = label
        self.selected = selected
        self.hint = hint
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(label)
                        .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                        .foregroundStyle(Palette.text)
                        .multilineTextAlignment(.leading)
                    if let hint, !hint.isEmpty {
                        Text(hint)
                            .scaledFont(size: 12, relativeTo: .caption)
                            .foregroundStyle(Palette.textMuted)
                            .multilineTextAlignment(.leading)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Text("\u{2713}")
                    .font(.body.weight(.bold))
                    .foregroundStyle(selected ? Palette.accent : Color.clear)
                    .frame(width: 18)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .background(selected ? Palette.accent.opacity(0.15) : Palette.surface)
            .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(selected ? Palette.accent : Palette.borderSubtle, lineWidth: 1)
            )
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

#Preview {
    VStack(spacing: 8) {
        ChoiceRow("Automatic", selected: true, hint: "Picks the best transport") {}
        ChoiceRow("Shadowsocks", selected: false) {}
    }
    .padding()
    .background(Palette.bg)
}
