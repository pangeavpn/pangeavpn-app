import Foundation

struct Region: Equatable, Sendable {
    let key: String
    let servers: [Server]

    // Missing load counts as fully loaded, so a node that reports always wins.
    var best: Server {
        servers.min { ($0.load ?? 100) < ($1.load ?? 100) } ?? servers[0]
    }
}

enum Regions {
    static let slotCount = 2
    // How many servers one start or switch may fall back through, the chosen one included.
    static let candidateLimit = 6

    static func group(_ servers: [Server]) -> [Region] {
        var order: [String] = []
        var byKey: [String: [Server]] = [:]
        for server in servers {
            let regionKey = key(of: server)
            if byKey[regionKey] == nil { order.append(regionKey) }
            byKey[regionKey, default: []].append(server)
        }
        return order.map { Region(key: $0, servers: byKey[$0] ?? []) }
    }

    static func region(of serverId: String, in regions: [Region]) -> Region? {
        regions.first { region in region.servers.contains { $0.id == serverId } }
    }

    // Regions keep only the nodes that can carry the transport; a region left empty drops out.
    static func supporting(_ transport: String, in regions: [Region]) -> [Region] {
        group(regions.flatMap(\.servers).filter { $0.supports(transport) })
    }

    // The selection always keeps a slot: a pick that vanished from view would read as lost.
    static func slots(_ regions: [Region], selectedKey: String?, count: Int = Regions.slotCount) -> [Region] {
        let head = Array(regions.prefix(count))
        guard let selectedKey, !head.contains(where: { $0.key == selectedKey }),
              let selected = regions.first(where: { $0.key == selectedKey })
        else { return head }
        return Array(([selected] + regions.filter { $0.key != selectedKey }).prefix(count))
    }

    static func resolveSelection(visible: [Server], previous: String, lastServerId: String?) -> String {
        if visible.contains(where: { $0.id == previous }) { return previous }
        if let lastServerId, visible.contains(where: { $0.id == lastServerId }) { return lastServerId }
        return ""
    }

    static func orderByRecent(_ regions: [Region], recent: [String]) -> [Region] {
        let ranked = recent.compactMap { key in regions.first { $0.key == key } }
        let rankedKeys = Set(ranked.map(\.key))
        return ranked + regions.filter { !rankedKeys.contains($0.key) }
    }

    static func promoteRecent(_ recent: [String], key: String, limit: Int = SharedPrefs.maxRecentRegions) -> [String] {
        Array(([key] + recent.filter { $0 != key }).prefix(limit))
    }

    // The chosen node, its siblings by load, then every later region in hub order, wrapping round.
    static func retryOrder(from serverId: String, in servers: [Server]) -> [String] {
        let regions = group(servers)
        guard let index = regions.firstIndex(where: { region in region.servers.contains { $0.id == serverId } }) else {
            return regions.flatMap { byLoad($0.servers).map(\.id) }
        }
        let siblings = byLoad(regions[index].servers).map(\.id).filter { $0 != serverId }
        let later = Array(regions[(index + 1)...]) + Array(regions[..<index])
        return [serverId] + siblings + later.flatMap { byLoad($0.servers).map(\.id) }
    }

    // One node per other region, so a short list still reaches other locations.
    static func candidates(from serverId: String, in servers: [Server], limit: Int = candidateLimit) -> [String] {
        let home = key(ofId: serverId)
        let order = retryOrder(from: serverId, in: servers).filter { $0 != serverId }
        let siblings = order.filter { key(ofId: $0) == home }
        let others = order.filter { key(ofId: $0) != home }
        let firstPerRegion = others.enumerated().compactMap { index, id -> String? in
            others[..<index].contains { key(ofId: $0) == key(ofId: id) } ? nil : id
        }
        return Array(([serverId] + siblings + firstPerRegion).prefix(limit))
    }

    // The line under a region's name, worded as on desktop.
    static func subtitle(for region: Region, selectedId: String?, pinnedId: String?) -> String {
        guard !region.servers.isEmpty else { return region.key }
        let count = region.servers.count
        guard let current = region.servers.first(where: { $0.id == selectedId }) else {
            guard count > 1 else { return region.best.id }
            let format = String(localized: "region_auto_of", defaultValue: "%d servers · auto")
            return String.localizedStringWithFormat(format, Int32(clamping: count))
        }
        if current.id == pinnedId {
            return String(format: String(localized: "region_pinned", defaultValue: "%1$@ · pinned"), current.id)
        }
        guard count > 1 else { return current.id }
        let format = String(localized: "region_best_of", defaultValue: "%1$@ · best of %2$@")
        return String(format: format, current.id, String(count))
    }

    static func key(of server: Server) -> String {
        key(ofId: server.id)
    }

    static func key(ofId id: String) -> String {
        id.replacingOccurrences(of: "-[0-9]+$", with: "", options: .regularExpression)
    }

    // Stable, so nodes reporting the same load keep their hub order.
    private static func byLoad(_ nodes: [Server]) -> [Server] {
        nodes.enumerated()
            .sorted { ($0.element.load ?? 100, $0.offset) < ($1.element.load ?? 100, $1.offset) }
            .map(\.element)
    }
}
