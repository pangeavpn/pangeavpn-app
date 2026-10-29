import SwiftUI

struct ConnectButton: View {
    let state: TunnelState
    let action: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var dimmed = false
    @State private var taps = 0

    init(state: TunnelState, action: @escaping () -> Void) {
        self.state = state
        self.action = action
    }

    private var busy: Bool { state == .connecting || state == .disconnecting }
    private var connected: Bool { state == .connected }

    private var label: String {
        switch state {
        case .connected:
            return String(localized: "hero_disconnect", defaultValue: "Disconnect")
        case .connecting:
            return String(localized: "hero_provisioning", defaultValue: "Provisioning...")
        case .disconnecting:
            return String(localized: "hero_disconnecting", defaultValue: "Disconnecting...")
        case .disconnected, .error:
            return String(localized: "hero_connect", defaultValue: "Connect")
        }
    }

    private var opacity: Double {
        guard busy else { return 1 }
        return reduceMotion ? 0.75 : (dimmed ? 0.55 : 1)
    }

    var body: some View {
        Button {
            taps += 1
            action()
        } label: {
            Text(label)
                .font(.system(.body, weight: .semibold))
                .foregroundStyle(connected ? Palette.textSecondary : Palette.onAccent)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 16)
                .background(connected ? Color.clear : Palette.accent, in: Capsule())
                .overlay(
                    Capsule().strokeBorder(connected ? Palette.border : Color.clear, lineWidth: 1)
                )
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .disabled(busy)
        .opacity(opacity)
        .sensoryFeedback(.impact(weight: .light), trigger: taps)
        .onAppear { updatePulse() }
        .onChange(of: busy) { _, _ in updatePulse() }
        .onChange(of: reduceMotion) { _, _ in updatePulse() }
    }

    private func updatePulse() {
        if busy && !reduceMotion {
            dimmed = false
            withAnimation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true)) {
                dimmed = true
            }
        } else {
            withAnimation(.easeOut(duration: 0.15)) { dimmed = false }
        }
    }
}

#Preview {
    VStack(spacing: 16) {
        ConnectButton(state: .disconnected) {}
        ConnectButton(state: .connecting) {}
        ConnectButton(state: .connected) {}
        ConnectButton(state: .disconnecting) {}
        ConnectButton(state: .error) {}
    }
    .padding(24)
    .background(Palette.bg)
}
