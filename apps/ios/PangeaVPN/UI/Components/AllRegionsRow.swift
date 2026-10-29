import SwiftUI

struct AllRegionsRow: View {
    let moreCount: Int
    let onOpen: () -> Void
    let onRefresh: () -> Void

    init(moreCount: Int, onOpen: @escaping () -> Void, onRefresh: @escaping () -> Void) {
        self.moreCount = moreCount
        self.onOpen = onOpen
        self.onRefresh = onRefresh
    }

    private let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)

    var body: some View {
        HStack(spacing: 8) {
            openButton
            Button(action: onRefresh) {
                Image(systemName: "arrow.clockwise")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(Palette.textSecondary)
                    .frame(width: 44, height: 44)
                    .contentShape(Circle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(String(localized: "region_refresh", defaultValue: "Refresh regions"))
        }
    }

    private var openButton: some View {
        Button(action: onOpen) {
            HStack(spacing: 12) {
                Image(systemName: "list.bullet")
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(Palette.textSecondary)
                    .frame(width: 18)
                    .accessibilityHidden(true)
                Text(String(localized: "hero_all_regions", defaultValue: "All regions"))
                    .font(.system(size: 15, weight: .semibold))
                    .foregroundStyle(Palette.textSecondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if moreCount > 0 {
                    Text(String(format: String(localized: "region_more", defaultValue: "%1$@ more"), String(moreCount)))
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(Palette.textSecondary)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 13)
            .overlay(shape.strokeBorder(Palette.border, style: StrokeStyle(lineWidth: 1, dash: [4, 4])))
            .contentShape(shape)
        }
        .buttonStyle(.plain)
    }
}

#Preview {
    VStack(spacing: 12) {
        AllRegionsRow(moreCount: 7, onOpen: {}, onRefresh: {})
        AllRegionsRow(moreCount: 0, onOpen: {}, onRefresh: {})
    }
    .padding(16)
    .background(Palette.bg)
}
