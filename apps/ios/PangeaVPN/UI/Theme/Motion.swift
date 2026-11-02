import SwiftUI

// One motion language on the map's own curve, so the map, hero, regions and button move as one.
enum Motion {
    // Layout that moves or resizes: sections sliding, rows arriving, lists opening.
    static func layout(_ reduce: Bool) -> Animation {
        reduce ? .easeInOut(duration: 0.2) : .spring(response: 0.5, dampingFraction: 0.86)
    }

    // Colour and text changing in place.
    static func state(_ reduce: Bool) -> Animation {
        .timingCurve(0.4, 0, 0.2, 1, duration: reduce ? 0.2 : 0.35)
    }

    // Small things appearing: notes, errors, toasts.
    static let quick = Animation.timingCurve(0.4, 0, 0.2, 1, duration: 0.2)

    // Whole screens swapping, such as splash to sign-in.
    static let screen = Animation.timingCurve(0.4, 0, 0.2, 1, duration: 0.3)

    // Text changing in place leaves fast and arrives gently (see FadeSwap).
    static let fadeOutTime = Duration.milliseconds(120)
    static let fadeOut = Animation.timingCurve(0.4, 0, 1, 1, duration: 0.12)
    static let fadeIn = Animation.timingCurve(0, 0, 0.2, 1, duration: 0.22)

    static func rise(_ reduce: Bool, by distance: CGFloat = 12) -> AnyTransition {
        reduce ? .opacity : .opacity.combined(with: .offset(y: distance))
    }
}
