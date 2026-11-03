import SwiftUI

// The app switcher snapshots the topmost screen, so every full-screen cover needs its own.
private struct PrivacyCover: ViewModifier {
    @Environment(\.scenePhase) private var scenePhase

    func body(content: Content) -> some View {
        content.overlay {
            if scenePhase != .active { SplashView() }
        }
    }
}

extension View {
    // Hides this screen behind the splash whenever the scene is not active.
    func privacyCover() -> some View {
        modifier(PrivacyCover())
    }
}
