import SwiftUI

struct GhostButton: View {
    let label: String
    let accent: Color?
    let action: () -> Void

    init(_ label: String, accent: Color? = nil, action: @escaping () -> Void) {
        self.label = label
        self.accent = accent
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(accent ?? Palette.text)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 14)
                .overlay(Capsule().strokeBorder(Palette.border, lineWidth: 1))
                .contentShape(Capsule())
        }
        .buttonStyle(PressFadeStyle())
        .accessibilityLabel(label)
    }
}

#Preview {
    VStack(spacing: 12) {
        GhostButton("Sign out") {}
        GhostButton("Remove device", accent: Palette.error) {}
    }
    .padding(24)
    .background(Palette.bg)
}
