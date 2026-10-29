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

    static func group(_ servers: [Server]) -> [Region] {
        var order: [String] = []
        var byKey: [String: [Server]] = [:]
        for server in servers {
            let key = regionKey(of: server)
            if byKey[key] == nil { order.append(key) }
            byKey[key, default: []].append(server)
        }
        return order.map { Region(key: $0, servers: byKey[$0] ?? []) }
    }

    static func region(of serverId: String, in regions: [Region]) -> Region? {
        regions.first { region in region.servers.contains { $0.id == serverId } }
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

    private static func regionKey(of server: Server) -> String {
        server.id.replacingOccurrences(of: "-[0-9]+$", with: "", options: .regularExpression)
    }
}
