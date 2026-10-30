import SwiftUI

// The picker's multihop switch and, while it is on, the entry choice.
struct MultihopPanel: View {
    let enabled: Bool
    let panel: RegionPickerModel.EntryPanel
    let onToggle: (Bool) -> Void
    let onChoose: (String?) -> Void

    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: 14, style: .continuous) }

    private static let title = String(localized: "regions_multihop", defaultValue: "Multihop")
    private static let hint = String(localized: "regions_multihop_hint", defaultValue: "Enter through one server, leave through another. The network you're on only ever sees the entry.")

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            head
            if enabled { entry }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(Palette.surface, in: shape)
        .overlay(shape.strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }

    private var head: some View {
        HStack(spacing: 12) {
            Image(systemName: "point.3.connected.trianglepath.dotted")
                .scaledFont(size: 18, weight: .medium, relativeTo: .body)
                .foregroundStyle(Palette.accent)
                .frame(width: 36, height: 36)
                .background(Palette.accent.opacity(0.15), in: RoundedRectangle(cornerRadius: 11, style: .continuous))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(Self.title)
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                    .foregroundStyle(Palette.text)
                Text(Self.hint)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
            .onTapGesture { onToggle(!enabled) }
            .accessibilityHidden(true)
            Toggle(Self.title, isOn: Binding(get: { enabled }, set: { onToggle($0) }))
                .labelsHidden()
                .tint(Palette.accent)
                .accessibilityLabel(Self.title)
                .accessibilityHint(Self.hint)
        }
    }

    private var entry: some View {
        VStack(alignment: .leading, spacing: 8) {
            SectionLabel(String(localized: "regions_entry", defaultValue: "Entry"))
            ChipFlow(spacing: 6) {
                ForEach(panel.chips) { chip in
                    EntryChipButton(chip: chip) { onChoose(chip.serverId) }
                }
            }
            if let text = hintText {
                Text(text)
                    .scaledFont(size: 11, design: .monospaced, relativeTo: .caption2)
                    .foregroundStyle(Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(.top, 12)
        .overlay(alignment: .top) {
            Rectangle().fill(Palette.borderSubtle).frame(height: 1)
        }
    }

    private var hintText: String? {
        switch panel.hint {
        case .hidden:
            return nil
        case .autoPicks(let region):
            return String(format: String(localized: "regions_entry_auto_picks", defaultValue: "Auto picks %@"), region)
        case .noEntries:
            return String(localized: "regions_no_entries", defaultValue: "No entry server is available right now")
        }
    }
}

private struct EntryChipButton: View {
    let chip: RegionPickerModel.EntryChip
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 7) {
                leading
                Text(label)
                    .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                    .lineLimit(1)
            }
            .foregroundStyle(chip.selected ? Palette.text : Palette.textSecondary)
            .padding(.leading, 9)
            .padding(.trailing, 12)
            .frame(minHeight: 34)
            .background(chip.selected ? Palette.accent.opacity(0.15) : Palette.elevated, in: Capsule())
            .overlay(Capsule().strokeBorder(chip.selected ? Palette.accent : Palette.border, lineWidth: 1))
            .padding(.vertical, 5)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(chip.isExit)
        .opacity(chip.isExit ? 0.4 : 1)
        .accessibilityAddTraits(chip.selected ? .isSelected : [])
        .accessibilityHint(chip.isExit ? String(localized: "regions_same_as_exit", defaultValue: "This is your exit region") : "")
    }

    @ViewBuilder
    private var leading: some View {
        if chip.id == .auto {
            Image(systemName: "sparkle")
                .scaledFont(size: 12, weight: .semibold, relativeTo: .caption)
                .foregroundStyle(Palette.accent)
                .accessibilityHidden(true)
        } else {
            Text(Flag.emoji(chip.country))
                .scaledFont(size: 15, relativeTo: .footnote)
                .accessibilityHidden(true)
        }
    }

    private var label: String {
        chip.id == .auto ? String(localized: "regions_entry_auto", defaultValue: "Auto") : chip.name
    }
}

// Wraps the chips onto as many lines as they need, like desktop's flex-wrap row.
private struct ChipFlow: Layout {
    let spacing: CGFloat

    private struct Line {
        var indices: [Int] = []
        var width: CGFloat = 0
        var height: CGFloat = 0
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let lines = arrange(subviews, width: proposal.width ?? .infinity)
        let width = lines.map(\.width).max() ?? 0
        return CGSize(width: width, height: lines.reduce(0) { $0 + $1.height })
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var y = bounds.minY
        for line in arrange(subviews, width: bounds.width) {
            var x = bounds.minX
            for index in line.indices {
                let size = subviews[index].sizeThatFits(ProposedViewSize(width: bounds.width, height: nil))
                subviews[index].place(at: CGPoint(x: x, y: y), anchor: .topLeading, proposal: ProposedViewSize(size))
                x += size.width + spacing
            }
            y += line.height
        }
    }

    private func arrange(_ subviews: Subviews, width: CGFloat) -> [Line] {
        var lines: [Line] = []
        var current = Line()
        for index in subviews.indices {
            let size = subviews[index].sizeThatFits(ProposedViewSize(width: width, height: nil))
            let needed = current.indices.isEmpty ? size.width : current.width + spacing + size.width
            if needed > width, !current.indices.isEmpty {
                lines.append(current)
                current = Line(indices: [index], width: size.width, height: size.height)
            } else {
                current.indices.append(index)
                current.width = needed
                current.height = max(current.height, size.height)
            }
        }
        if !current.indices.isEmpty { lines.append(current) }
        return lines
    }
}

#if DEBUG
private let panelPreviewServers = [
    Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 22, multihop: true),
    Server(id: "de-fra-1", name: "Frankfurt", region: "eu", country: "DE", load: 48, multihop: true),
    Server(id: "nl-ams-1", name: "Amsterdam", region: "eu", country: "NL", load: 15, multihop: true),
    Server(id: "us-nyc-1", name: "New York", region: "us", country: "US", load: 73, multihop: true),
]

private struct MultihopPanelPreview: View {
    @State private var enabled = true
    @State private var chosen: String?

    var body: some View {
        MultihopPanel(
            enabled: enabled,
            panel: RegionPickerModel.entryPanel(
                visible: Regions.group(panelPreviewServers), exitId: "us-nyc-1", chosenId: chosen),
            onToggle: { enabled = $0 },
            onChoose: { chosen = $0 })
            .padding(16)
            .frame(maxHeight: .infinity, alignment: .top)
            .background(Palette.bg)
    }
}

#Preview("Multihop") {
    MultihopPanelPreview()
}
#endif
