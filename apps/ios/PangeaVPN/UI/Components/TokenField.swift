import SwiftUI

struct TokenField: View {
    @Binding var text: String
    let placeholder: String
    let enabled: Bool
    let isError: Bool
    let onSubmit: () -> Void

    @FocusState private var focused: Bool

    init(
        text: Binding<String>,
        placeholder: String,
        enabled: Bool = true,
        isError: Bool = false,
        onSubmit: @escaping () -> Void = {}
    ) {
        self._text = text
        self.placeholder = placeholder
        self.enabled = enabled
        self.isError = isError
        self.onSubmit = onSubmit
    }

    private static let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)

    var body: some View {
        TextField(
            "",
            text: $text,
            prompt: Text(placeholder).foregroundColor(Palette.textMuted)
        )
        .scaledFont(size: 17, design: .monospaced, relativeTo: .body)
        .tracking(2.4)
        .foregroundStyle(Palette.text)
        .tint(Palette.accent)
        .focused($focused)
        .allowsHitTesting(enabled)
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled(true)
        .keyboardType(.asciiCapable)
        .privacySensitive()
        .submitLabel(.go)
        .onSubmit(onSubmit)
        .onChange(of: text) { _, newValue in
            let clean = TokenInput.sanitize(newValue)
            if clean != newValue { text = clean }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 15)
        .background(focused ? Palette.accent.opacity(0.1) : Palette.surface, in: Self.shape)
        .overlay(Self.shape.strokeBorder(borderColor, lineWidth: focused ? 2 : 1))
        .clipShape(Self.shape)
    }

    private var borderColor: Color {
        if isError { return Palette.error }
        return focused ? Palette.accent : Palette.borderSubtle
    }
}

#if DEBUG
private struct TokenFieldPreview: View {
    @State private var filled = "ABCD1234EFGH5678"

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
