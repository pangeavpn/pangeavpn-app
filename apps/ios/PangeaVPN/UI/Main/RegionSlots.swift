import SwiftUI

struct RegionSlots: View {
    @Environment(ServersStore.self) private var servers
    @Environment(ConnectionStore.self) private var connection
    @Environment(SettingsStore.self) private var settings
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let rows: Int
    @Binding var metrics: SlotMetrics

    init(rows: Int = Regions.slotCount, metrics: Binding<SlotMetrics> = .constant(SlotMetrics())) {
        self.rows = rows
        self._metrics = metrics
    }

    private var transport: String { settings.settings.preferredTransport }

    private var heading: String {
        settings.multihopEnabled
            ? String(localized: "main_exit_region", defaultValue: "Exit region")
            : String(localized: "hero_region", defaultValue: "Region")
    }

    // The store ignores a pick while a start or switch is under way, so the rows say so too.
    private var working: Bool { connection.busy || connection.switching }

    var body: some View {
        let slots = servers.slots(for: transport, count: rows)
        let total = servers.regions(for: transport).count
        let selectedKey = servers.selectedRegionKey
        let lead = slots.first?.key
        VStack(alignment: .leading, spacing: SlotBudget.spacing) {
            SectionLabel(heading)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { metrics.label = $0 }
            if slots.isEmpty {
                EmptyRegions(loading: servers.loading, error: servers.error, onRefresh: refresh)
            } else {
                ForEach(slots, id: \.key) { region in
                    row(region, selected: region.key == selectedKey)
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height in
                            if region.key == lead { metrics.row = height }
                        }
                        .transition(rowTransition)
                }
            }
            AllRegionsRow(
                moreCount: max(total - slots.count, 0),
                canRotate: connection.canRotate,
                onOpen: { connection.showServerSheet = true },
                onRotate: rotate
            )
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { metrics.door = $0 }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var rowTransition: AnyTransition {
        reduceMotion ? .opacity : .opacity.combined(with: .offset(y: 14))
    }

    private func row(_ region: Region, selected: Bool) -> some View {
        let selectedId = servers.selected?.id
        let pinnedId = servers.pinnedServerId
        let node = RegionPickerModel.node(for: region, selectedId: selectedId, pinnedId: pinnedId)
        return ServerRow(
            name: RegionPickerModel.name(of: region),
            country: RegionPickerModel.country(of: region),
            load: node.load,
            selected: selected,
            subtitle: Regions.subtitle(for: region, selectedId: selectedId, pinnedId: pinnedId),
            action: { activate(region) }
        )
        .disabled(working)
        .opacity(working ? 0.6 : 1)
    }

    private func activate(_ region: Region) {
        Task { await connection.activate(region: region) }
    }

    private func rotate() {
        Task { await connection.rotate() }
    }

    private func refresh() {
        Task { await servers.refresh() }
    }
}

private struct EmptyRegions: View {
    let loading: Bool
    let error: String?
    let onRefresh: () -> Void

    private let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)

    init(loading: Bool, error: String?, onRefresh: @escaping () -> Void) {
        self.loading = loading
        self.error = error
        self.onRefresh = onRefresh
    }

    var body: some View {
        HStack(spacing: 12) {
            if loading {
                ProgressView()
                    .frame(maxWidth: .infinity)
            } else {
                VStack(alignment: .leading, spacing: 2) {
                    Text(String(localized: "hero_no_servers", defaultValue: "No servers available"))
                        .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                        .foregroundStyle(Palette.textSecondary)
                    if let error, !error.isEmpty {
                        Text(error)
                            .scaledFont(size: 12, relativeTo: .caption)
                            .foregroundStyle(Palette.textMuted)
                            .lineLimit(2)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Button(action: onRefresh) {
                    Image(systemName: "arrow.clockwise")
                        .scaledFont(size: 16, weight: .semibold, relativeTo: .callout)
                        .foregroundStyle(Palette.textSecondary)
                        .frame(width: 44, height: 44)
                        .contentShape(Circle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(String(localized: "region_refresh", defaultValue: "Refresh regions"))
            }
        }
        .padding(.leading, 14)
        .padding(.trailing, 6)
        .padding(.vertical, 5)
        .frame(minHeight: 54)
        .background(Palette.surface, in: shape)
        .overlay(shape.strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }
}

#if DEBUG
private let slotPreviewServers: [Server] = [
    Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 41),
    Server(id: "uk-lon-2", name: "London", region: "eu", country: "GB", load: 61),
    Server(id: "nl-ams-1", name: "Amsterdam", region: "eu", country: "NL", load: 33),
    Server(id: "nl-ams-2", name: "Amsterdam", region: "eu", country: "NL", load: 52),
    Server(id: "us-nyc-1", name: "New York", region: "us", country: "US", load: 73),
    Server(id: "sg-1", name: "Singapore", region: "ap", country: "SG", load: nil),
]

private struct RegionSlotsPreview: View {
    let stores: MainPreviewStores

    var body: some View {
        stores.attach(
            RegionSlots()
                .padding(20)
                .frame(maxHeight: .infinity, alignment: .top)
                .background(Palette.bg)
        )
    }
}

#Preview("Slots") {
    RegionSlotsPreview(stores: MainPreviewStores(servers: slotPreviewServers))
}

#Preview("Selection pulled in") {
    RegionSlotsPreview(stores: MainPreviewStores(
        status: MainPreviewData.status(.connected, serverId: "us-nyc-1", serverName: "New York"),
        servers: slotPreviewServers))
}

#Preview("No servers") {
    RegionSlotsPreview(stores: MainPreviewStores(servers: []))
}
#endif
