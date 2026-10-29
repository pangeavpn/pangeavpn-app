import SwiftUI
import UIKit

struct SettingsField: View {
    @Binding private var text: String
    private let placeholder: String
    private let keyboard: UIKeyboardType
    private let onCommit: () -> Void
    @FocusState private var focused: Bool

    init(
        text: Binding<String>,
        placeholder: String,
        keyboard: UIKeyboardType = .default,
        onCommit: @escaping () -> Void = {}
    ) {
        self._text = text
        self.placeholder = placeholder
        self.keyboard = keyboard
        self.onCommit = onCommit
    }

    private var needsDoneKey: Bool { keyboard == .numberPad || keyboard == .decimalPad }

    var body: some View {
        TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(Palette.textMuted))
            .font(.system(size: 15))
            .foregroundStyle(Palette.text)
            .tint(Palette.accent)
            .keyboardType(keyboard)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .submitLabel(.done)
            .focused($focused)
            .onSubmit { focused = false }
            .onChange(of: focused) { _, isFocused in
                if !isFocused { onCommit() }
            }
            .toolbar {
                ToolbarItemGroup(placement: .keyboard) {
                    if needsDoneKey && focused {
                        Spacer()
                        Button(String(localized: "settings_done", defaultValue: "Done")) {
                            focused = false
                        }
                    }
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 13)
            .background(Palette.surface)
            .clipShape(RoundedRectangle(cornerRadius: 14))
            .overlay(
                RoundedRectangle(cornerRadius: 14)
                    .strokeBorder(focused ? Palette.accent : Palette.border, lineWidth: focused ? 2 : 1)
            )
            .contentShape(RoundedRectangle(cornerRadius: 14))
            .onTapGesture { focused = true }
    }
}

private struct SettingsFieldPreview: View {
    @State private var dns = ""
    @State private var mtu = "1280"

    var body: some View {
        VStack(spacing: 8) {
            SettingsField(text: $dns, placeholder: "1.1.1.1")
            SettingsField(text: $mtu, placeholder: "1280", keyboard: .numberPad)
        }
        .padding()
        .background(Palette.bg)
    }
}

#Preview {
    SettingsFieldPreview()
}
