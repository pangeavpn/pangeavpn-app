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

private struct KickerFace: Equatable, Sendable {
    let state: TunnelState
    let postQuantum: Bool
    let split: Bool
}

struct HeroKicker: View {
    let state: TunnelState
    let postQuantum: Bool
    let split: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var dimmed = false

    init(state: TunnelState, postQuantum: Bool = false, split: Bool = false) {
        self.state = state
        self.postQuantum = postQuantum
        self.split = split
    }

    private var pulsing: Bool {
        !reduceMotion && (state == .connecting || state == .disconnecting)
    }

    var body: some View {
        // Dot, label and chips swap as one face; the hidden chip keeps the row's height,
        // so a chip arriving never pushes the headline down.
        ZStack(alignment: .leading) {
            HeroChip.postQuantum.hidden().accessibilityHidden(true)
            FadeSwap(KickerFace(state: state, postQuantum: postQuantum, split: split)) { face in
                HStack(spacing: 9) {
                    dot(face.state)
                    HStack(spacing: 8) {
                        Text(HeroText.label(face.state).uppercased())
                            .kerning(1.8)
                            .scaledFont(size: 11, weight: .bold, relativeTo: .caption2)
                            .lineLimit(1)
                            .foregroundStyle(HeroText.accent(face.state))
                        if face.postQuantum { HeroChip.postQuantum }
                        if face.split { HeroChip.split }
                    }
                }
            }
        }
        // Moved as one frame: otherwise a label resized mid-slide is re-timed apart from the dot.
        .geometryGroup()
        .accessibilityElement(children: .combine)
        .padding(.leading, -4)
        .onAppear { updatePulse() }
        .onChange(of: pulsing) { _, _ in updatePulse() }
    }

    private func dot(_ state: TunnelState) -> some View {
        let color = state == .disconnected ? Palette.off : HeroText.accent(state)
        return Circle()
            .fill(color)
            .frame(width: 8, height: 8)
            .padding(4)
            .background(Circle().fill(color.opacity(state == .connected ? 0.2 : 0)))
            .opacity(pulsing && dimmed ? 0.4 : 1)
    }

    private func updatePulse() {
        if pulsing {
            withAnimation(.easeInOut(duration: 0.8).repeatForever(autoreverses: true)) { dimmed = true }
        } else {
            withAnimation(Motion.quick) { dimmed = false }
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
            .lineLimit(2)
            .minimumScaleFactor(0.75)
            .foregroundStyle(Palette.text)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

struct HeroDetail: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    // An empty line still keeps its height, so the screen does not jump as states change.
    var body: some View {
        Text(text.isEmpty ? " " : text)
            .scaledFont(size: 13, relativeTo: .footnote)
            .lineSpacing(3)
            .lineLimit(2)
            .truncationMode(.tail)
            .foregroundStyle(Palette.textMuted)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityHidden(text.isEmpty)
    }
}

private let previewStates: [TunnelState] = [.disconnected, .connecting, .connected, .disconnecting, .error]

#Preview("Kicker") {
    VStack(alignment: .leading, spacing: 12) {
        ForEach(previewStates, id: \.self) { HeroKicker(state: $0) }
        HeroKicker(state: .connected, postQuantum: true, split: true)
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
        HeroDetail(HeroDetailText.pausedLine)
    }
    .padding()
    .background(Palette.bg)
}
