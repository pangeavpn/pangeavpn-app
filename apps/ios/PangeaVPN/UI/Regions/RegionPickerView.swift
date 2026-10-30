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
        let empty = RegionPickerModel.empty(visible: regions, all: servers.regions)
        VStack(alignment: .leading, spacing: 16) {
            if empty == nil, let message = servers.error {
                InfoNote(message, systemImage: "exclamationmark.circle", tint: Palette.error)
            }
            if multihopAvailable { multihopPanel(regions) }
            if let empty {
                EmptyRegionList(
                    empty: empty,
                    error: servers.error,
                    loading: servers.loading,
                    onRefresh: refresh)
            } else {
                regionSections(regions)
            }
        }
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

    private func refresh() {
        let store = servers
        Task { await store.refresh() }
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

// Stands in for the list when nothing can be shown, with the last refresh error and a retry.
private struct EmptyRegionList: View {
    let empty: RegionPickerModel.Empty
    let error: String?
    let loading: Bool
    let onRefresh: () -> Void

    private let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: empty == .noServers ? "server.rack" : "line.3.horizontal.decrease.circle")
                .scaledFont(size: 28, weight: .medium, relativeTo: .title2)
                .foregroundStyle(Palette.textMuted)
                .accessibilityHidden(true)
            Text(message)
                .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                .foregroundStyle(Palette.textSecondary)
                .fixedSize(horizontal: false, vertical: true)
            if let error, !error.isEmpty {
                Text(error)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.error)
                    .fixedSize(horizontal: false, vertical: true)
            }
            refreshButton
                .padding(.top, 6)
        }
        .multilineTextAlignment(.center)
        .frame(maxWidth: .infinity)
        .padding(.horizontal, 20)
        .padding(.vertical, 28)
        .background(Palette.surface, in: shape)
        .overlay(shape.strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }

    private var message: String {
        empty == .noServers
            ? String(localized: "hero_no_servers", defaultValue: "No servers available")
            : String(localized: "server_picker_none_for_transport", defaultValue: "No servers support this transport")
    }

    // The spinner swaps in over the arrow, so the button keeps its size while a refresh runs.
    private var refreshButton: some View {
        let label = String(localized: "region_refresh", defaultValue: "Refresh regions")
        return Button(action: onRefresh) {
            HStack(spacing: 8) {
                ZStack {
                    Image(systemName: "arrow.clockwise")
                        .opacity(loading ? 0 : 1)
                    if loading {
                        ProgressView()
                            .controlSize(.small)
                            .tint(Palette.textSecondary)
                    }
                }
                .accessibilityHidden(true)
                Text(label)
            }
            .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
            .foregroundStyle(Palette.text)
            .padding(.horizontal, 18)
            .padding(.vertical, 8)
            .frame(minHeight: 44)
            .overlay(Capsule().strokeBorder(Palette.border, lineWidth: 1))
            .contentShape(Capsule())
        }
        .buttonStyle(PressFadeStyle())
        .disabled(loading)
        .accessibilityLabel(label)
        .accessibilityAddTraits(loading ? .updatesFrequently : [])
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
