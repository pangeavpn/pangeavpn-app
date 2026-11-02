import SwiftUI

// Changes content by fading the old out before the new fades in, so two values never overprint.
struct FadeSwap<Value: Equatable & Sendable, Content: View>: View {
    private let value: Value
    private let content: (Value) -> Content
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var shown: Value
    @State private var visible = true

    init(_ value: Value, @ViewBuilder content: @escaping (Value) -> Content) {
        self.value = value
        self.content = content
        _shown = State(initialValue: value)
    }

    var body: some View {
        content(shown)
            .contentTransition(.identity)
            .opacity(visible ? 1 : 0)
            .task(id: value) { @MainActor in await swap(to: value) }
    }

    // A newer value cancels this task mid-fade, and the next one picks up from invisible.
    private func swap(to next: Value) async {
        guard next != shown else {
            if !visible { withAnimation(Motion.fadeIn) { visible = true } }
            return
        }
        if reduceMotion {
            shown = next
            return
        }
        withAnimation(Motion.fadeOut) { visible = false }
        try? await Task.sleep(for: Motion.fadeOutTime)
        guard !Task.isCancelled else { return }
        withAnimation(Motion.layout(false)) { shown = next }
        withAnimation(Motion.fadeIn) { visible = true }
    }
}
