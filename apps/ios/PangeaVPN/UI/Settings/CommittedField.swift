import SwiftUI
import UIKit

// A text setting that only applies on Save or Return, so half-typed values never reach the core.
struct CommittedField: View {
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

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                SectionLabel(label)
                Spacer()
                if text != initial {
                    Button { onCommit(text) } label: {
                        Text(String(localized: "settings_save", defaultValue: "Save"))
                            .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                            .foregroundStyle(Palette.accent)
                            .padding(.horizontal, 12)
                            .frame(minHeight: 44)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(PressFadeStyle())
                }
            }
            .frame(minHeight: 44)
            SettingsField(
                text: Binding(get: { text }, set: { text = transform($0) }),
                placeholder: placeholder,
                keyboard: keyboard
            )
            .onSubmit { if text != initial { onCommit(text) } }
            .padding(.top, 6)
            if !hint.isEmpty {
                Text(hint)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.textMuted)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 6)
            }
        }
        .onChange(of: initial) { _, value in text = value }
    }
}
