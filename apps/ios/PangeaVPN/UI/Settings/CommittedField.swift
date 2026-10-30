import SwiftUI
import UIKit

// A text setting that only applies on Save or Return, so half-typed values never reach the core.
struct CommittedField: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let label: String
    let hint: String
    let placeholder: String
    let initial: String
    let keyboard: UIKeyboardType
    let transform: (String) -> String
    let onCommit: (String) -> Void

    @State private var text: String

    init(
        label: String,
        hint: String,
        placeholder: String,
        initial: String,
        keyboard: UIKeyboardType,
        transform: @escaping (String) -> String = { $0 },
        onCommit: @escaping (String) -> Void
    ) {
        self.label = label
        self.hint = hint
        self.placeholder = placeholder
        self.initial = initial
        self.keyboard = keyboard
        self.transform = transform
        self.onCommit = onCommit
        _text = State(initialValue: initial)
    }

    private var dirty: Bool { text != initial }

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            if typeSize.isAccessibilitySize {
                field
                if dirty { saveButton(leadingInset: 4) }
            } else {
                HStack(spacing: 4) {
                    field
                    if dirty { saveButton(leadingInset: 12) }
                }
            }
            if !hint.isEmpty {
                Text(hint)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.textMuted)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, 4)
            }
        }
        .onChange(of: initial) { _, value in text = value }
    }

    private var field: some View {
        SettingsField(
            text: Binding(get: { text }, set: { text = transform($0) }),
            placeholder: placeholder,
            keyboard: keyboard
        )
        .onSubmit { if dirty { onCommit(text) } }
        .accessibilityLabel(label)
    }

    private func saveButton(leadingInset: CGFloat) -> some View {
        Button { onCommit(text) } label: {
            Text(String(localized: "settings_save", defaultValue: "Save"))
                .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                .foregroundStyle(Palette.accent)
                .lineLimit(1)
                .fixedSize(horizontal: true, vertical: false)
                .padding(.leading, leadingInset)
                .padding(.trailing, 12)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(PressFadeStyle())
    }
}
