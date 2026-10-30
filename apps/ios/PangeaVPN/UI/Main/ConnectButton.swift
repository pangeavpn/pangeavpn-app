import SwiftUI

struct ConnectButton: View {
    let kind: MainAction
    let action: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var dimmed = false
    @State private var taps = 0

    init(kind: MainAction, action: @escaping () -> Void) {
        self.kind = kind
        self.action = action
    }

    private var pulsing: Bool { kind == .provisioning || kind == .disconnecting }
    private var enabled: Bool { kind == .connect || kind == .stop || kind == .disconnect }
    // Leaving is normal, so the ways out stay quiet, as desktop's Disconnect does.
    private var primary: Bool { kind == .connect || kind == .provisioning }

    private var label: String {
        switch kind {
        case .connect, .lockdown, .hidden:
            return String(localized: "hero_connect", defaultValue: "Connect")
        case .provisioning:
            return String(localized: "hero_provisioning", defaultValue: "Provisioning...")
        case .stop:
            return String(localized: "main_stop", defaultValue: "Stop")
        case .disconnect:
            return String(localized: "hero_disconnect", defaultValue: "Disconnect")
        case .disconnecting:
            return String(localized: "hero_disconnecting", defaultValue: "Disconnecting...")
        }
    }

    private var opacity: Double {
        guard pulsing else { return 1 }
        return reduceMotion ? 0.75 : (dimmed ? 0.55 : 1)
    }

    var body: some View {
        Button {
            taps += 1
            action()
        } label: {
            Text(label)
                .scaledFont(size: 17, weight: .semibold, relativeTo: .body)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .foregroundStyle(primary ? Palette.onAccent : Palette.textSecondary)
                .frame(maxWidth: .infinity, minHeight: 52)
                .background(primary ? Palette.accent : Color.clear, in: Capsule())
                .overlay(Capsule().strokeBorder(primary ? Color.clear : Palette.border, lineWidth: 1))
                .contentShape(Capsule())
        }
        .buttonStyle(PressFadeStyle())
        .disabled(!enabled)
        .opacity(opacity)
        .sensoryFeedback(.impact(weight: .light), trigger: taps)
        .accessibilityAddTraits(pulsing ? .updatesFrequently : [])
        .onAppear { updatePulse() }
        .onChange(of: pulsing) { _, _ in updatePulse() }
        .onChange(of: reduceMotion) { _, _ in updatePulse() }
    }

    private func updatePulse() {
        if pulsing && !reduceMotion {
            dimmed = false
            withAnimation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true)) {
                dimmed = true
            }
        } else {
            withAnimation(.easeOut(duration: 0.15)) { dimmed = false }
        }
    }
}

// Lockdown hides Disconnect; the note says why and leads to the switch that turns it off.
struct LockdownNote: View {
    let onOpenSettings: () -> Void

    init(onOpenSettings: @escaping () -> Void) {
        self.onOpenSettings = onOpenSettings
    }

    var body: some View {
        Button(action: onOpenSettings) {
            HStack(spacing: 12) {
                Image(systemName: "lock.fill")
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .body)
                    .foregroundStyle(Palette.accent)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 1) {
                    Text(String(localized: "main_lockdown_note", defaultValue: "Lockdown is on"))
                        .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                        .foregroundStyle(Palette.text)
                    Text(String(localized: "main_lockdown_hint", defaultValue: "Turn it off in Settings to disconnect."))
                        .scaledFont(size: 12, relativeTo: .caption)
                        .foregroundStyle(Palette.textMuted)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: "chevron.right")
                    .scaledFont(size: 12, weight: .semibold, relativeTo: .caption)
                    .foregroundStyle(Palette.textMuted)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 18)
            .padding(.vertical, 8)
            .frame(minHeight: 52)
            .background(Palette.surface, in: Capsule())
            .overlay(Capsule().strokeBorder(Palette.border, lineWidth: 1))
            .contentShape(Capsule())
        }
        .buttonStyle(PressFadeStyle())
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isButton)
    }
}

#Preview {
    VStack(spacing: 14) {
        ConnectButton(kind: .connect) {}
        ConnectButton(kind: .provisioning) {}
        ConnectButton(kind: .stop) {}
        ConnectButton(kind: .disconnect) {}
        ConnectButton(kind: .disconnecting) {}
        LockdownNote {}
    }
    .padding(24)
    .background(Palette.bg)
}
