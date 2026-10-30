import SwiftUI

// One region in the picker; with two or more nodes a chevron discloses them, as on desktop.
struct RegionPickerRow: View {
    let name: String
    let country: String
    let subtitle: String
    let load: Int?
    let selected: Bool
    let role: RegionPickerModel.RowRole
    let nodes: [RegionPickerModel.NodeEntry]
    let expanded: Bool
    let enabled: Bool
    let onSelect: () -> Void
    let onToggleNodes: () -> Void
    let onSelectNode: (Server) -> Void

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: 14, style: .continuous) }

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                regionButton
                if !nodes.isEmpty { disclosureButton }
            }
            .fixedSize(horizontal: false, vertical: true)
            if expanded && !nodes.isEmpty { nodeList }
        }
        .background(selected ? Palette.accent.opacity(0.15) : Palette.surface, in: shape)
        .overlay(shape.strokeBorder(selected ? Palette.accent : Palette.borderSubtle, lineWidth: 1))
        .clipShape(shape)
        .dynamicTypeSize(...DynamicTypeSize.xxxLarge)
    }

    private var regionButton: some View {
        Button(action: onSelect) {
            HStack(spacing: 12) {
                Text(Flag.emoji(country))
                    .scaledFont(size: 22, relativeTo: .title2)
                    .accessibilityHidden(true)
                titles
                if let load { PickerLoadMeter(load: load) }
                trailingMark
            }
            .padding(.leading, 14)
            .padding(.trailing, nodes.isEmpty ? 14 : 10)
            .padding(.vertical, 12)
            .frame(minHeight: 56)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!enabled || role == .lockedEntry)
        .opacity(enabled && role != .lockedEntry ? 1 : 0.55)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var titles: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(name)
                .scaledFont(size: 16, weight: .semibold, relativeTo: .callout)
                .foregroundStyle(Palette.text)
                .lineLimit(1)
                .truncationMode(.tail)
            if !subtitle.isEmpty {
                Text(subtitle)
                    .scaledFont(size: 11, design: .monospaced, relativeTo: .caption2)
                    .foregroundStyle(Palette.textMuted)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private var trailingMark: some View {
        if role == .plain {
            Text("\u{2713}")
                .fontWeight(.bold)
                .foregroundStyle(selected ? Palette.accent : Color.clear)
                .frame(width: 18)
                .accessibilityHidden(true)
        } else {
            PickerTag(String(localized: "regions_entry_badge", defaultValue: "Entry"))
        }
    }

    private var disclosureButton: some View {
        Button(action: onToggleNodes) {
            Image(systemName: "chevron.down")
                .scaledFont(size: 14, weight: .semibold, relativeTo: .footnote)
                .foregroundStyle(Palette.textMuted)
                .rotationEffect(.degrees(expanded ? 180 : 0))
                .frame(width: 44)
                .frame(minHeight: 44, maxHeight: .infinity)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .overlay(alignment: .leading) {
            Rectangle().fill(Palette.borderSubtle).frame(width: 1)
        }
        .accessibilityLabel(disclosureLabel)
    }

    private var disclosureLabel: String {
        let format = expanded
            ? String(localized: "regions_hide_servers", defaultValue: "Hide servers in %@")
            : String(localized: "regions_show_servers", defaultValue: "Show servers in %@")
        return String(format: format, name)
    }

    private var nodeList: some View {
        VStack(spacing: 0) {
            ForEach(nodes) { entry in
                NodeRow(serverId: entry.server.id, badge: entry.badge, load: entry.server.load) {
                    onSelectNode(entry.server)
                }
                .disabled(!enabled)
            }
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 4)
        .overlay(alignment: .top) {
            Rectangle().fill(Palette.borderSubtle).frame(height: 1)
        }
    }
}

// The picker's load bar and percentage, coloured like the main screen's.
struct PickerLoadMeter: View {
    let load: Int

    private var pct: Int { min(max(load, 0), 100) }

    var body: some View {
        HStack(spacing: 8) {
            Capsule()
                .fill(Palette.border)
                .frame(width: 42, height: 5)
                .overlay(alignment: .leading) {
                    Capsule().fill(LoadTone.color(forPercent: pct)).frame(width: 42 * CGFloat(pct) / 100, height: 5)
                }
            Text(verbatim: "\(pct)%")
                .scaledFont(size: 13, relativeTo: .footnote)
                .foregroundStyle(Palette.textMuted)
                .frame(minWidth: 34, alignment: .trailing)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(format: String(localized: "regions_load", defaultValue: "Server load %@"), "\(pct)%"))
    }
}

// A small outlined caps label, as desktop's region badges.
struct PickerTag: View {
    private let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        Text(text.uppercased())
            .scaledFont(size: 9, weight: .bold, relativeTo: .caption2)
            .tracking(1)
            .foregroundStyle(Palette.accent)
            .lineLimit(1)
            .padding(.horizontal, 5)
            .padding(.vertical, 2)
            .overlay(RoundedRectangle(cornerRadius: 4, style: .continuous).strokeBorder(Palette.accent, lineWidth: 1))
            .fixedSize()
    }
}

#if DEBUG
private let rowPreviewNodes = [
    RegionPickerModel.NodeEntry(server: Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 22), badge: .best),
    RegionPickerModel.NodeEntry(server: Server(id: "uk-lon-2", name: "London", region: "eu", country: "GB", load: 61), badge: .pinned),
]

#Preview("Rows") {
    VStack(spacing: 8) {
        RegionPickerRow(name: "London", country: "GB", subtitle: "uk-lon-2 · pinned", load: 61, selected: true,
                        role: .plain, nodes: rowPreviewNodes, expanded: true, enabled: true,
                        onSelect: {}, onToggleNodes: {}, onSelectNode: { _ in })
        RegionPickerRow(name: "Amsterdam", country: "NL", subtitle: "nl-ams-1", load: 15, selected: false,
                        role: .lockedEntry, nodes: [], expanded: false, enabled: true,
                        onSelect: {}, onToggleNodes: {}, onSelectNode: { _ in })
        RegionPickerRow(name: "Frankfurt", country: "DE", subtitle: "2 servers · auto", load: 48, selected: false,
                        role: .plain, nodes: rowPreviewNodes, expanded: false, enabled: true,
                        onSelect: {}, onToggleNodes: {}, onSelectNode: { _ in })
        RegionPickerRow(name: "Tokyo", country: "JP", subtitle: "jp-tyo-1", load: nil, selected: false,
                        role: .plain, nodes: [], expanded: false, enabled: false,
                        onSelect: {}, onToggleNodes: {}, onSelectNode: { _ in })
    }
    .padding(16)
    .background(Palette.bg)
}
#endif
