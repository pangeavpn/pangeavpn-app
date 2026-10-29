import SwiftUI

struct ServerSheet: View {
    @Environment(ServersStore.self) private var servers
    @Environment(\.dismiss) private var dismiss

    init() {}

    var body: some View {
        ScrollView {
            if servers.regions.isEmpty {
                Text(String(localized: "hero_no_servers", defaultValue: "No servers available"))
                    .foregroundStyle(Palette.textSecondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(24)
            } else {
                LazyVStack(spacing: 9) {
                    ForEach(servers.regions, id: \.key) { region in
                        row(region)
                    }
                }
                .padding(.horizontal, 20)
                .padding(.vertical, 24)
            }
        }
        .refreshable { await servers.refresh() }
        .background(Palette.bg)
        .presentationDetents([.medium, .large])
        .presentationBackground(Palette.bg)
    }

    private func row(_ region: Region) -> some View {
        let node = region.best
        let name = region.servers.first.map { $0.name.isEmpty ? region.key : $0.name } ?? region.key
        return ServerRow(
            name: name,
            country: region.servers.first?.country ?? "",
            load: node.load,
            selected: region.key == servers.selectedRegionKey,
            subtitle: node.id,
            action: {
                Task {
                    await servers.select(node)
                    dismiss()
                }
            }
        )
    }
}

#if DEBUG
@MainActor
private func previewServers() -> ServersStore {
    let core = FakeCore()
    core.serversResult = .success([
        Server(id: "eu-west-1", name: "London", region: "eu", country: "GB", load: 20),
        Server(id: "eu-west-2", name: "London", region: "eu", country: "GB", load: 55),
        Server(id: "us-east-1", name: "New York", region: "us", country: "US", load: 80),
        Server(id: "de-1", name: "Frankfurt", region: "eu", country: "DE", load: nil)
    ])
    return ServersStore(core: core)
}

private struct SheetPreview: View {
    @State private var store = previewServers()

    var body: some View {
        ServerSheet()
            .environment(store)
            .task { await store.refresh() }
    }
}

#Preview("Regions") {
    SheetPreview()
}

#Preview("Empty") {
    ServerSheet()
        .environment(ServersStore(core: FakeCore()))
}
#endif
