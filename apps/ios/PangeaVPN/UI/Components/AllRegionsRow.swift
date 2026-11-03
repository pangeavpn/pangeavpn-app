import SwiftUI

// The door to the full list, with desktop's rotate button beside it.
struct AllRegionsRow: View {
    let moreCount: Int
    let canRotate: Bool
    let onOpen: () -> Void
    let onRotate: () -> Void

    init(moreCount: Int, canRotate: Bool, onOpen: @escaping () -> Void, onRotate: @escaping () -> Void) {
        self.moreCount = moreCount
        self.canRotate = canRotate
        self.onOpen = onOpen
        self.onRotate = onRotate
    }

    private let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)

    var body: some View {
        HStack(spacing: 8) {
            openButton
            rotateButton
        }
        .fixedSize(horizontal: false, vertical: true)
    }

    private var openButton: some View {
        Button(action: onOpen) {
            HStack(spacing: 12) {
                Image(systemName: "list.bullet")
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .callout)
                    .foregroundStyle(Palette.textSecondary)
                    .frame(width: 24)
                    .accessibilityHidden(true)
                Text(String(localized: "hero_all_regions", defaultValue: "All regions"))
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                    .foregroundStyle(Palette.textSecondary)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if moreCount > 0 {
                    Text(String(format: String(localized: "region_more", defaultValue: "%1$@ more"), String(moreCount)))
                        .scaledFont(size: 12, design: .monospaced, relativeTo: .caption)
                        .foregroundStyle(Palette.textSecondary)
                        .lineLimit(1)
                }
            }
            .padding(.horizontal, 14)
            .frame(maxHeight: .infinity)
            .frame(minHeight: 48)
            .overlay(shape.strokeBorder(Palette.border, style: StrokeStyle(lineWidth: 1, dash: [4, 4])))
            .contentShape(shape)
        }
        .buttonStyle(PressFadeStyle())
    }

    private var rotateButton: some View {
        Button(action: onRotate) {
            Image(systemName: "arrow.left.arrow.right")
                .scaledFont(size: 16, weight: .semibold, relativeTo: .callout)
                .foregroundStyle(Palette.textSecondary)
                .frame(width: 48)
                .frame(maxHeight: .infinity)
                .frame(minHeight: 48)
                .background(Palette.surface, in: shape)
                .overlay(shape.strokeBorder(Palette.borderSubtle, lineWidth: 1))
                .contentShape(shape)
        }
        .buttonStyle(PressFadeStyle())
        .disabled(!canRotate)
        .opacity(canRotate ? 1 : 0.4)
        .accessibilityLabel(String(localized: "main_rotate", defaultValue: "Switch to another server"))
    }
}

#Preview {
    VStack(spacing: 12) {
        AllRegionsRow(moreCount: 7, canRotate: true, onOpen: {}, onRotate: {})
        AllRegionsRow(moreCount: 0, canRotate: false, onOpen: {}, onRotate: {})
    }
    .padding(16)
    .background(Palette.bg)
}
