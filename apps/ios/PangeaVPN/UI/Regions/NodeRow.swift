import SwiftUI

// One server inside an expanded region; a tap pins it.
struct NodeRow: View {
    let serverId: String
    let badge: RegionPickerModel.NodeBadge?
    let load: Int?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 9) {
                Text(serverId)
                    .scaledFont(size: 12, design: .monospaced, relativeTo: .caption)
                    .foregroundStyle(Palette.textSecondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let badge { PickerTag(Self.label(for: badge)) }
                Spacer(minLength: 8)
                if let load { PickerLoadMeter(load: load) }
            }
            .padding(.horizontal, 8)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    static func label(for badge: RegionPickerModel.NodeBadge) -> String {
        switch badge {
        case .pinned: return String(localized: "regions_badge_pinned", defaultValue: "Pinned")
        case .best: return String(localized: "regions_badge_best", defaultValue: "Best")
        }
    }
}

#if DEBUG
#Preview {
    VStack(spacing: 0) {
        NodeRow(serverId: "uk-lon-1", badge: .best, load: 22, action: {})
        NodeRow(serverId: "uk-lon-2", badge: .pinned, load: 61, action: {})
        NodeRow(serverId: "uk-lon-3", badge: nil, load: 93, action: {})
        NodeRow(serverId: "uk-lon-4", badge: nil, load: nil, action: {})
    }
    .padding(16)
    .background(Palette.bg)
}
#endif
