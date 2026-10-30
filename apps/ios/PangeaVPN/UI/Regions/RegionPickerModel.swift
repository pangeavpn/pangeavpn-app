import Foundation

// The region picker's decisions, kept free of views and stores; mirrors desktop's regions.ts and multihop.ts.
enum RegionPickerModel {
    enum Empty: Equatable {
        case noServers
        case noneForTransport
    }

    struct Sections: Equatable {
        let recent: [Region]
        let rest: [Region]
    }

    enum NodeBadge: Equatable {
        case pinned
        case best
    }

    struct NodeEntry: Equatable, Identifiable {
        let server: Server
        let badge: NodeBadge?

        var id: String { server.id }
    }

    // A hand-picked entry cannot also be the exit, so its row is locked; an automatic one just moves.
    enum RowRole: Equatable {
        case plain
        case entry
        case lockedEntry
    }

    enum EntryChoice: Hashable {
        case auto
        case region(String)
    }

    struct EntryChip: Equatable, Identifiable {
        let id: EntryChoice
        let name: String
        let country: String
        let serverId: String?
        let selected: Bool
        let isExit: Bool
    }

    enum EntryHint: Equatable {
        case hidden
        case autoPicks(String)
        case noEntries
    }

    struct EntryPanel: Equatable {
        let chips: [EntryChip]
        let hint: EntryHint
    }

    static func sections(_ regions: [Region], recentKeys: [String]) -> Sections {
        let recent = recentKeys.reduce(into: [Region]()) { found, key in
            guard !found.contains(where: { $0.key == key }),
                  let region = regions.first(where: { $0.key == key }) else { return }
            found.append(region)
        }
        let recentKeySet = Set(recent.map(\.key))
        return Sections(recent: recent, rest: regions.filter { !recentKeySet.contains($0.key) })
    }

    static func empty(visible: [Region], all: [Region]) -> Empty? {
        guard visible.isEmpty else { return nil }
        return all.isEmpty ? .noServers : .noneForTransport
    }

    // The first named node without its ordinal, so a region never reads as one node ("Amsterdam 1").
    static func name(of region: Region) -> String {
        guard let named = region.servers.first(where: { !$0.name.isEmpty })?.name else { return region.key }
        return named.replacingOccurrences(of: "\\s+[0-9]+$", with: "", options: .regularExpression)
    }

    static func country(of region: Region) -> String {
        region.servers.first { !$0.country.isEmpty }?.country ?? ""
    }

    static func holdsSelection(_ region: Region, selectedId: String?) -> Bool {
        guard let selectedId else { return false }
        return region.servers.contains { $0.id == selectedId }
    }

    static func node(for region: Region, selectedId: String?, pinnedId: String?) -> Server {
        if let pinned = region.servers.first(where: { $0.id == pinnedId }) { return pinned }
        if let current = region.servers.first(where: { $0.id == selectedId }) { return current }
        return region.best
    }

    static func nodes(of region: Region, pinnedId: String?) -> [NodeEntry] {
        guard region.servers.count > 1 else { return [] }
        let bestId = region.best.id
        return region.servers.map { server in
            let badge: NodeBadge? = server.id == pinnedId ? .pinned : (server.id == bestId ? .best : nil)
            return NodeEntry(server: server, badge: badge)
        }
    }

    static func multihopAvailable(_ regions: [Region]) -> Bool {
        regions.contains { region in region.servers.contains { $0.multihop } }
    }

    // Relays outside the exit's region, lightest first and in hub order on ties.
    static func entryCandidates(_ servers: [Server], exitId: String) -> [Server] {
        let exitRegion = Regions.key(ofId: exitId)
        return servers.enumerated()
            .filter { $0.element.multihop && Regions.key(ofId: $0.element.id) != exitRegion }
            .sorted { lhs, rhs in
                let left = lhs.element.load ?? 100
                let right = rhs.element.load ?? 100
                return left == right ? lhs.offset < rhs.offset : left < right
            }
            .map { $0.element }
    }

    static func resolveEntry(_ servers: [Server], exitId: String, chosenId: String?) -> Server? {
        let candidates = entryCandidates(servers, exitId: exitId)
        return candidates.first { $0.id == chosenId } ?? candidates.first
    }

    static func entryPanel(visible: [Region], exitId: String, chosenId: String?) -> EntryPanel {
        let servers = visible.flatMap(\.servers)
        let candidates = entryCandidates(servers, exitId: exitId)
        let chosen = candidates.first { $0.id == chosenId }
        let chosenRegion = chosen.map { Regions.key(ofId: $0.id) }
        let exitRegion = exitId.isEmpty ? nil : Regions.key(ofId: exitId)
        let regionChips = Regions.group(servers.filter { $0.multihop }).map { region in
            EntryChip(id: .region(region.key), name: name(of: region), country: country(of: region),
                      serverId: region.best.id, selected: region.key == chosenRegion,
                      isExit: region.key == exitRegion)
        }
        let auto = EntryChip(id: .auto, name: "", country: "", serverId: nil, selected: chosen == nil, isExit: false)
        return EntryPanel(chips: [auto] + regionChips, hint: hint(chosen: chosen, auto: candidates.first, visible: visible))
    }

    static func role(of region: Region, entry: Server?, chosenId: String?, selectedId: String?) -> RowRole {
        guard !holdsSelection(region, selectedId: selectedId), let entry,
              region.servers.contains(where: { $0.id == entry.id }) else { return .plain }
        return entry.id == chosenId ? .lockedEntry : .entry
    }

    private static func hint(chosen: Server?, auto: Server?, visible: [Region]) -> EntryHint {
        if chosen != nil { return .hidden }
        guard let auto else { return .noEntries }
        return .autoPicks(Regions.region(of: auto.id, in: visible).map(name(of:)) ?? auto.name)
    }
}
