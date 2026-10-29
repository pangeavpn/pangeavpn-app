import SwiftUI

enum HeroText {
    static func accent(_ state: TunnelState) -> Color {
        switch state {
        case .connected: return Palette.connected
        case .connecting, .disconnecting: return Palette.connecting
        case .error: return Palette.error
        case .disconnected: return Palette.textMuted
        }
    }

    static func emphasised(_ template: String, emphasis: String) -> AttributedString {
        guard let slot = template.range(of: "%1$@") else { return AttributedString(template) }
        var word = AttributedString(emphasis)
        word.inlinePresentationIntent = .stronglyEmphasized
        var result = AttributedString(String(template[..<slot.lowerBound]))
        result.append(word)
        result.append(AttributedString(String(template[slot.upperBound...])))
        return result
    }

    static func label(_ state: TunnelState) -> String {
        switch state {
        case .connected: return String(localized: "state_connected", defaultValue: "CONNECTED")
        case .connecting: return String(localized: "state_connecting", defaultValue: "CONNECTING")
        case .disconnecting: return String(localized: "state_disconnecting", defaultValue: "DISCONNECTING")
        case .error: return String(localized: "state_error", defaultValue: "ERROR")
        case .disconnected: return String(localized: "state_disconnected", defaultValue: "DISCONNECTED")
        }
    }

    static func headline(_ state: TunnelState) -> String {
        switch state {
        case .connected: return String(localized: "hero_headline_connected", defaultValue: "The world is %1$@")
        case .connecting: return String(localized: "hero_headline_connecting", defaultValue: "Drawing it %1$@")
        case .disconnecting: return String(localized: "hero_headline_disconnecting", defaultValue: "Letting it %1$@")
        case .error: return String(localized: "hero_headline_error", defaultValue: "Couldn't %1$@")
        case .disconnected: return String(localized: "hero_headline_disconnected", defaultValue: "The world is %1$@")
        }
    }

    static func emphasisWord(_ state: TunnelState) -> String {
        switch state {
        case .connected: return String(localized: "hero_emphasis_connected", defaultValue: "whole")
        case .connecting: return String(localized: "hero_emphasis_connecting", defaultValue: "together")
        case .disconnecting: return String(localized: "hero_emphasis_disconnecting", defaultValue: "drift")
        case .error: return String(localized: "hero_emphasis_error", defaultValue: "connect")
        case .disconnected: return String(localized: "hero_emphasis_disconnected", defaultValue: "apart")
        }
    }
}

struct HeroKicker: View {
    let state: TunnelState
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var dimmed = false

    init(state: TunnelState) {
        self.state = state
    }

    private var pulsing: Bool {
        !reduceMotion && (state == .connecting || state == .disconnecting)
    }

    var body: some View {
        HStack(spacing: 9) {
            Circle()
                .fill(state == .disconnected ? Palette.off : HeroText.accent(state))
                .frame(width: 8, height: 8)
                .opacity(pulsing && dimmed ? 0.4 : 1)
                .animation(reduceMotion ? nil : .easeInOut(duration: 0.4), value: state)
            Text(HeroText.label(state).uppercased())
                .scaledFont(size: 11, weight: .bold, relativeTo: .caption2)
                .kerning(1.8)
                .foregroundStyle(HeroText.accent(state))
        }
        .accessibilityElement(children: .combine)
        .onAppear { updatePulse() }
        .onChange(of: pulsing) { _, _ in updatePulse() }
    }

    private func updatePulse() {
        if pulsing {
            withAnimation(.easeInOut(duration: 0.8).repeatForever(autoreverses: true)) { dimmed = true }
        } else {
            withAnimation(.easeOut(duration: 0.2)) { dimmed = false }
        }
    }
}

struct HeroHeadline: View {
    let state: TunnelState
    @ScaledMetric(relativeTo: .largeTitle) private var size: CGFloat = 34

    init(state: TunnelState) {
        self.state = state
    }

    var body: some View {
        Text(HeroText.emphasised(HeroText.headline(state), emphasis: HeroText.emphasisWord(state)))
            .font(.system(size: size, weight: .light))
            .kerning(-1)
            .lineSpacing(4)
            .foregroundStyle(Palette.text)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct HeroDetail: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text)
            .scaledFont(size: 13, relativeTo: .footnote)
            .lineSpacing(3)
            .lineLimit(2)
            .truncationMode(.tail)
            .foregroundStyle(Palette.textMuted)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private let previewStates: [TunnelState] = [.disconnected, .connecting, .connected, .disconnecting, .error]

#Preview("Kicker") {
    VStack(alignment: .leading, spacing: 12) {
        ForEach(previewStates, id: \.self) { HeroKicker(state: $0) }
    }
    .padding()
    .background(Palette.bg)
}

#Preview("Headline") {
    VStack(alignment: .leading, spacing: 12) {
        ForEach(previewStates, id: \.self) { HeroHeadline(state: $0) }
    }
    .padding()
    .background(Palette.bg)
}

#Preview("Detail") {
    VStack(alignment: .leading, spacing: 12) {
        ForEach(previewStates, id: \.self) { state in
            HeroDetail("\(HeroText.label(state)): a long detail line that wraps onto a second line and then truncates with an ellipsis")
        }
    }
    .padding()
    .background(Palette.bg)
}
