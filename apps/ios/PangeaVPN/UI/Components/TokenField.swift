import SwiftUI

struct TokenField: View {
    @Binding var text: String
    let placeholder: String
    let label: String?
    let enabled: Bool
    let isError: Bool
    let onSubmit: () -> Void

    @FocusState private var focused: Bool

    init(
        text: Binding<String>,
        placeholder: String,
        label: String? = nil,
        enabled: Bool = true,
        isError: Bool = false,
        onSubmit: @escaping () -> Void = {}
    ) {
        self._text = text
        self.placeholder = placeholder
        self.label = label
        self.enabled = enabled
        self.isError = isError
        self.onSubmit = onSubmit
    }

    private static let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)

    var body: some View {
        // The placeholder takes part in layout, hidden once there is text, so the field is always
        // tall enough for a whole account number, at every text size.
        ZStack(alignment: .topLeading) {
            placeholderText
                .opacity(text.isEmpty ? 1 : 0)
            field
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 15)
        .background(focused ? Palette.accent.opacity(0.1) : Palette.surface, in: Self.shape)
        .overlay(Self.shape.strokeBorder(borderColor, lineWidth: focused ? 2 : 1))
        .clipShape(Self.shape)
    }

    private var field: some View {
        TextField("", text: $text, axis: .vertical)
        // A full account number wraps on every iPhone at default size; reserving two lines stops the field jumping.
        .lineLimit(2...)
        .scaledFont(size: 17, design: .monospaced, relativeTo: .body)
        .tracking(2.4)
        .foregroundStyle(Palette.text)
        .accessibilityLabel(label ?? placeholder)
        .tint(Palette.accent)
        .focused($focused)
        .allowsHitTesting(enabled)
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled(true)
        .keyboardType(.asciiCapable)
        .privacySensitive()
        .submitLabel(.go)
        .onSubmit(submit)
        .onChange(of: text) { oldValue, newValue in
            let clean = TokenInput.sanitize(newValue)
            if clean != newValue { text = clean }
            if TokenInput.isReturnPress(old: oldValue, new: newValue) { submit() }
        }
    }

    // A prompt stays on one line and truncates on narrow phones, so the field draws its own that wraps.
    private var placeholderText: some View {
        GroupedCode(code: placeholder)
            .scaledFont(size: 17, design: .monospaced, relativeTo: .body)
            .tracking(2.4)
            .foregroundStyle(Palette.textMuted)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    // Dropping focus matches a single-line field, whose Return also dismisses the keyboard.
    private func submit() {
        focused = false
        onSubmit()
    }

    private var borderColor: Color {
        if isError { return Palette.error }
        return focused ? Palette.accent : Palette.borderSubtle
    }
}

#if DEBUG
private struct TokenFieldPreview: View {
    @State private var filled = "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01"

    var body: some View {
        VStack(spacing: 16) {
            TokenField(text: .constant(""), placeholder: "Account number")
            TokenField(text: $filled, placeholder: "Account number")
            TokenField(text: $filled, placeholder: "Account number", isError: true)
            TokenField(text: $filled, placeholder: "Account number", enabled: false)
        }
        .padding(16)
        .background(Palette.bg)
    }
}

#Preview {
    TokenFieldPreview()
}
#endif
