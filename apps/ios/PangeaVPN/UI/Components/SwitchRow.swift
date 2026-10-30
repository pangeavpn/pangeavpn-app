import SwiftUI

struct SwitchRow: View {
    private let title: String
    private let hint: String?
    @Binding private var isOn: Bool

    init(_ title: String, isOn: Binding<Bool>, hint: String? = nil) {
        self.title = title
        self._isOn = isOn
        self.hint = hint
    }

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
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
            .contentShape(Rectangle())
            .onTapGesture { isOn.toggle() }
            .accessibilityHidden(true)
            Toggle(title, isOn: $isOn)
                .labelsHidden()
                .tint(Palette.accent)
                .accessibilityLabel(title)
                .accessibilityHint(hint ?? "")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(Palette.surface)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }
}

private struct SwitchRowPreview: View {
    @State private var on = true
    @State private var off = false

    var body: some View {
        VStack(spacing: 8) {
            SwitchRow("Kill switch", isOn: $on, hint: "Applies on the next connect")
            SwitchRow("Auto-connect", isOn: $off)
        }
        .padding()
        .background(Palette.bg)
    }
}

#Preview {
    SwitchRowPreview()
}
