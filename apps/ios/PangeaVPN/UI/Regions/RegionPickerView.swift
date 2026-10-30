import SwiftUI

// The full-screen region list, with the multihop options on top when any server can relay.
struct RegionPickerView: View {
    @Environment(ServersStore.self) private var servers
    @Environment(SettingsStore.self) private var settings
    @Environment(ConnectionStore.self) private var connection
    @Environment(\.dismiss) private var dismiss
    @State private var expanded: Set<String> = []
    @State private var pendingRoute: Task<Void, Never>?

    // A burst of toggles and entry picks becomes one re-dial, as on desktop.
    private static let routeDebounce = Duration.milliseconds(600)

    init() {}

    var body: some View {
        NavigationStack {
            ScrollView {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.top, 8)
                    .padding(.bottom, 32)
            }
            .refreshable { await servers.refresh() }
            .background(Palette.bg.ignoresSafeArea())
            .navigationTitle(String(localized: "regions_title", defaultValue: "Select region"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Palette.bg, for: .navigationBar)
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    SettingsCloseButton { dismiss() }
                }
            }
        }
        .preferredColorScheme(settings.darkTheme.map { $0 ? ColorScheme.dark : ColorScheme.light })
        .task { await servers.refresh() }
        .toastOverlay()
        .privacyCover()
    }

    // The store ignores a pick while a start or switch is under way, so the rows say so too.
    private var working: Bool { connection.busy || connection.switching }

    private var visible: [Region] {
        servers.regions(for: settings.settings.preferredTransport)
    }

    private var multihopAvailable: Bool {
        RegionPickerModel.multihopAvailable(servers.regions)
    }

    private var exitId: String {
        servers.selected?.id ?? ""
    }

    @ViewBuilder
    private var content: some View {
        let regions = visible
        VStack(alignment: .leading, spacing: 16) {
            if let message = servers.error {
                InfoNote(message, systemImage: "exclamationmark.circle", tint: Palette.error)
            }
            if let empty = RegionPickerModel.empty(visible: regions, all: servers.regions) {
                emptyState(empty)
            } else {
                if multihopAvailable { multihopPanel(regions) }
                regionSections(regions)
            }
        }
    }

    private func emptyState(_ empty: RegionPickerModel.Empty) -> some View {
        let text = empty == .noServers
            ? String(localized: "hero_no_servers", defaultValue: "No servers available")
            : String(localized: "server_picker_none_for_transport", defaultValue: "No servers support this transport")
        return Text(text)
            .scaledFont(size: 15, relativeTo: .subheadline)
            .foregroundStyle(Palette.textMuted)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 24)
    }

    private func multihopPanel(_ regions: [Region]) -> some View {
        MultihopPanel(
            enabled: settings.multihopEnabled,
            panel: RegionPickerModel.entryPanel(visible: regions, exitId: exitId, chosenId: settings.multihopEntryId),
            onToggle: { setMultihop($0) },
            onChoose: { chooseEntry($0) })
    }

    private func regionSections(_ regions: [Region]) -> some View {
        let sections = RegionPickerModel.sections(regions, recentKeys: servers.recentRegionKeys)
        let multihopOn = settings.multihopEnabled && multihopAvailable
        let entry = multihopOn
            ? RegionPickerModel.resolveEntry(regions.flatMap(\.servers), exitId: exitId, chosenId: settings.multihopEntryId)
            : nil
        let restTitle = multihopOn
            ? String(localized: "regions_exit_region", defaultValue: "Exit region")
            : String(localized: "hero_all_regions", defaultValue: "All regions")
        return VStack(alignment: .leading, spacing: 20) {
            if !sections.recent.isEmpty {
                section(String(localized: "regions_recent", defaultValue: "Recent"), sections.recent, entry: entry, multihopOn: multihopOn)
            }
            if !sections.rest.isEmpty {
                section(restTitle, sections.rest, entry: entry, multihopOn: multihopOn)
            }
        }
    }

    private func section(_ title: String, _ regions: [Region], entry: Server?, multihopOn: Bool) -> some View {
        VStack(alignment: .leading, spacing: 9) {
            SectionLabel(title)
                .padding(.leading, 4)
            ForEach(regions, id: \.key) { region in
                row(region, entry: entry, multihopOn: multihopOn)
            }
        }
    }

    private func row(_ region: Region, entry: Server?, multihopOn: Bool) -> some View {
        let selectedId = servers.selected?.id
        let pinnedId = servers.pinnedServerId
        let node = RegionPickerModel.node(for: region, selectedId: selectedId, pinnedId: pinnedId)
        let role = multihopOn
            ? RegionPickerModel.role(of: region, entry: entry, chosenId: settings.multihopEntryId, selectedId: selectedId)
            : .plain
        return RegionPickerRow(
            name: RegionPickerModel.name(of: region),
            country: RegionPickerModel.country(of: region),
            subtitle: Regions.subtitle(for: region, selectedId: selectedId, pinnedId: pinnedId),
            load: node.load,
            selected: RegionPickerModel.holdsSelection(region, selectedId: selectedId),
            role: role,
            nodes: RegionPickerModel.nodes(of: region, pinnedId: pinnedId),
            expanded: expanded.contains(region.key),
            enabled: !working,
            onSelect: { activate(region) },
            onToggleNodes: { toggleNodes(region.key) },
            onSelectNode: { activate($0) })
    }

    private func toggleNodes(_ key: String) {
        if expanded.contains(key) {
            expanded = expanded.subtracting([key])
        } else {
            expanded = expanded.union([key])
        }
    }

    private func activate(_ region: Region) {
        guard !working else { return }
        let store = connection
        dismiss()
        Task { await store.activate(region: region) }
    }

    private func activate(_ server: Server) {
        guard !working else { return }
        let store = connection
        dismiss()
        Task { await store.activate(server: server) }
    }

    private func setMultihop(_ enabled: Bool) {
        settings.setMultihopEnabled(enabled)
        scheduleRouteChange()
    }

    private func chooseEntry(_ serverId: String?) {
        settings.setMultihopEntry(serverId)
        scheduleRouteChange()
    }

    // Left running past dismissal, so a change made just before picking a region still applies.
    private func scheduleRouteChange() {
        pendingRoute?.cancel()
        let store = connection
        pendingRoute = Task {
            do {
                try await Task.sleep(for: Self.routeDebounce)
            } catch {
                return
            }
            await store.routeChanged()
        }
    }
}

#if DEBUG
private let pickerPreviewServers: [Server] = [
    Server(id: "uk-lon-1", name: "London", region: "eu", country: "GB", load: 22, multihop: true),
    Server(id: "uk-lon-2", name: "London", region: "eu", country: "GB", load: 61, multihop: true),
    Server(id: "de-fra-1", name: "Frankfurt", region: "eu", country: "DE", load: 48, multihop: true),
    Server(id: "nl-ams-1", name: "Amsterdam", region: "eu", country: "NL", load: 15, multihop: true),
    Server(id: "us-nyc-1", name: "New York", region: "us", country: "US", load: 73),
    Server(id: "us-nyc-2", name: "New York", region: "us", country: "US", load: 40),
    Server(id: "sg-1", name: "Singapore", region: "ap", country: "SG", load: 35),
    Server(id: "jp-tyo-1", name: "Tokyo", region: "ap", country: "JP", load: nil),
]

private struct PickerPreviewHost: View {
    let servers: ServersStore
    let settings: SettingsStore
    let connection: ConnectionStore
    let toasts: ToastCenter

    var body: some View {
        RegionPickerView()
            .environment(servers)
            .environment(settings)
            .environment(connection)
            .environment(toasts)
            .task { await settings.load() }
    }
}

@MainActor
private func pickerPreview(_ list: [Server], suite: String, selected: String = "", transport: String = "auto",
                           multihop: Bool = false, recent: [String] = []) -> PickerPreviewHost {
    let defaults = UserDefaults(suiteName: suite) ?? .standard
    defaults.removePersistentDomain(forName: suite)
    defaults.set(recent, forKey: SharedPrefs.Key.recentRegions)
    let core = FakeCore()
    var config = AppSettings()
    config.lastServerId = selected
    config.preferredTransport = transport
    core.configResult = .success(config)
    core.serversResult = .success(list)
    let tunnel = FakeTunnel()
    let settings = SettingsStore(core: core, defaults: defaults)
    settings.setMultihopEnabled(multihop)
    let servers = ServersStore(core: core, defaults: defaults)
    let session = SessionStore(core: core, tunnel: tunnel)
    return PickerPreviewHost(
        servers: servers,
        settings: settings,
        connection: ConnectionStore(tunnel: tunnel, servers: servers, settings: settings, session: session),
        toasts: ToastCenter())
}

#Preview("Regions") {
    pickerPreview(pickerPreviewServers, suite: "preview.regions", selected: "us-nyc-1", recent: ["us-nyc", "sg"])
}

#Preview("Multihop") {
    pickerPreview(pickerPreviewServers, suite: "preview.regions.multihop", selected: "us-nyc-1", multihop: true)
}

#Preview("No servers") {
    pickerPreview([], suite: "preview.regions.empty")
}

#Preview("None for transport") {
    pickerPreview(pickerPreviewServers, suite: "preview.regions.transport", transport: "reality")
}
#endif
