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

    private static func regionKey(of server: Server) -> String {
        server.id.replacingOccurrences(of: "-[0-9]+$", with: "", options: .regularExpression)
    }
}
