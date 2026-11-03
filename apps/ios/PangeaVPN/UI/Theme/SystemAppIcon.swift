import UIKit

// The live Home Screen icon, behind the protocol the tests fake.
@MainActor
struct SystemAppIcon: AppIconSwitching {
    var supportsAlternateIcons: Bool { UIApplication.shared.supportsAlternateIcons }
    var alternateIconName: String? { UIApplication.shared.alternateIconName }

    func setAlternateIconName(_ name: String?) async throws {
        try await UIApplication.shared.setAlternateIconName(name)
    }
}
