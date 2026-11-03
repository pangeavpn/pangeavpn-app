import SwiftUI

struct BrandLogo: View {
    var size: CGFloat = 32
    var tint: Color = Palette.accent

    var body: some View {
        Image("Logo")
            .renderingMode(.template)
            .resizable()
            .scaledToFit()
            .foregroundStyle(tint)
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}
