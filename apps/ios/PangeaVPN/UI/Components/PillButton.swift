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

    // A faded accent still reads as live in dark mode, so idle disabled goes neutral; busy keeps the accent.
    private var muted: Bool { !enabled && !loading }

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
                        .scaledFont(size: 17, weight: .semibold, relativeTo: .body)
                        .foregroundStyle(muted ? Palette.textMuted : Palette.onAccent)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 16)
            .background(muted ? Palette.surface : Palette.accent, in: Capsule())
            .overlay(Capsule().strokeBorder(muted ? Palette.border : .clear, lineWidth: 1))
            .contentShape(Capsule())
        }
        .buttonStyle(PressFadeStyle())
        .disabled(!enabled || loading)
        .accessibilityLabel(label)
        .accessibilityAddTraits(loading ? .updatesFrequently : [])
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
