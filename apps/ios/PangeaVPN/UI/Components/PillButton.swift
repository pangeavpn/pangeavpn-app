import SwiftUI

struct PillButton: View {
    let label: String
    let enabled: Bool
    let loading: Bool
    let action: () -> Void

    init(_ label: String, enabled: Bool = true, loading: Bool = false, action: @escaping () -> Void) {
        self.label = label
        self.enabled = enabled
        self.loading = loading
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            ZStack {
                if loading {
                    ProgressView()
                        .progressViewStyle(.circular)
                        .tint(Palette.onAccent)
                        .frame(width: 22, height: 22)
                } else {
                    Text(label)
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundStyle(Palette.onAccent)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 16)
            .background(Palette.accent.opacity(enabled ? 1 : 0.4), in: Capsule())
            .contentShape(Capsule())
        }
        .buttonStyle(PressFadeStyle())
        .disabled(!enabled || loading)
        .accessibilityLabel(label)
    }
}

struct PressFadeStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.opacity(configuration.isPressed ? 0.8 : 1)
    }
}

#Preview {
    VStack(spacing: 12) {
        PillButton("Continue") {}
        PillButton("Continue", enabled: false) {}
        PillButton("Continue", loading: true) {}
    }
    .padding(24)
    .background(Palette.bg)
}
