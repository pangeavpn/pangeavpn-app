import SwiftUI

// One rule for when a screen gets the desktop-style iPad layout, so every screen switches together.
enum AppLayout {
    // Below this an iPad window (split view, slide over) is phone-sized, so it gets the iPhone layout.
    static let wideMinimumWidth: CGFloat = 700
    // Sign-in style screens stop stretching past this on a wide window.
    static let readableWidth: CGFloat = 520

    static func isWide(width: CGFloat, sizeClass: UserInterfaceSizeClass?) -> Bool {
        sizeClass != .compact && width >= wideMinimumWidth
    }
}

// Picks the wide or compact body from the space it is given, cross-fading when a resize crosses over.
struct AdaptiveLayout<Wide: View, Compact: View>: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    private let wide: () -> Wide
    private let compact: () -> Compact

    init(@ViewBuilder wide: @escaping () -> Wide, @ViewBuilder compact: @escaping () -> Compact) {
        self.wide = wide
        self.compact = compact
    }

    var body: some View {
        GeometryReader { proxy in
            let isWide = AppLayout.isWide(width: proxy.size.width, sizeClass: sizeClass)
            ZStack {
                if isWide {
                    wide().transition(.opacity)
                } else {
                    compact().transition(.opacity)
                }
            }
            .frame(width: proxy.size.width, height: proxy.size.height)
            .animation(Motion.screen, value: isWide)
        }
    }
}

extension View {
    // Centres content at a comfortable width instead of stretching it across a wide window.
    func readableWidth() -> some View {
        frame(maxWidth: AppLayout.readableWidth).frame(maxWidth: .infinity)
    }
}
