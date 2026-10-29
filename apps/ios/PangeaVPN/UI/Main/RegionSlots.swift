import SwiftUI

struct RegionSlots: View {
    @Environment(ServersStore.self) private var servers
    @Environment(ConnectionStore.self) private var connection

    init() {}

    var body: some View {
        let slots = servers.slots
        let selectedKey = servers.selectedRegionKey
        VStack(alignment: .leading, spacing: 0) {
            if slots.isEmpty {
                Text(String(localized: "hero_no_servers", defaultValue: "No servers available"))
                    .font(.body)
                    .foregroundStyle(Palette.textMuted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.vertical, 12)
            } else {
                VStack(spacing: 9) {
                    ForEach(slots, id: \.key) { region in
                        row(region, selected: region.key == selectedKey)
                    }
                }
            }
            AllRegionsRow(
                moreCount: max(servers.regions.count - slots.count, 0),
                onOpen: { connection.showServerSheet = true },
                onRefresh: refresh
            )
            .padding(.top, 9)
        }
    }

    private func row(_ region: Region, selected: Bool) -> some View {
        let node = region.best
        return ServerRow(
            name: Self.name(of: region),
            country: region.servers.first?.country ?? "",
            load: node.load,
            selected: selected,
            subtitle: node.id,
            action: { select(node) }
        )
    }

    private func select(_ server: Server) {
        Task { await servers.select(server) }
    }

    private func refresh() {
        Task { await servers.refresh() }
    }

    private static func name(of region: Region) -> String {
        guard let first = region.servers.first, !first.name.isEmpty else { return region.key }
        return first.name
    }
}

#if DEBUG
private let slotPreviewServers: [Server] = [
    Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 22),
    Server(id: "uk-lon-2", name: "London", region: "eu", country: "GB", load: 61),
    Server(id: "de-fra-1", name: "Frankfurt", region: "eu", country: "DE", load: 48),
    Server(id: "us-nyc-1", name: "New York", region: "us", country: "US", load: 73),
    Server(id: "sg-1", name: "Singapore", region: "ap", country: "SG", load: nil),
]

private struct RegionSlotsPreview: View {
    let servers: ServersStore
    let connection: ConnectionStore

    var body: some View {
        RegionSlots()
            .environment(servers)
            .environment(connection)
            .padding(20)
            .frame(maxHeight: .infinity, alignment: .top)
            .background(Palette.bg)
            .task { await servers.refresh() }
    }
}

@MainActor
private func regionSlotsPreview(_ list: [Server], lastServerId: String = "") -> RegionSlotsPreview {
    let core = FakeCore()
    var config = AppSettings()
    config.lastServerId = lastServerId
    core.configResult = .success(config)
    core.serversResult = .success(list)
    let servers = ServersStore(core: core)
    let connection = ConnectionStore(tunnel: FakeTunnel(), servers: servers, settings: SettingsStore(core: core))
    return RegionSlotsPreview(servers: servers, connection: connection)
}

#Preview("Slots") {
    regionSlotsPreview(slotPreviewServers)
}

#Preview("Selection pulled in") {
    regionSlotsPreview(slotPreviewServers, lastServerId: "us-nyc-1")
}

#Preview("No servers") {
    regionSlotsPreview([])
}
#endif
