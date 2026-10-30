import Foundation

struct RotationPlan: Equatable, Sendable {
    // Servers to try in order: this cycle's fresh ones, then the visited ones as a fallback.
    let plan: [String]
    // The visited list to carry forward; cleared once every alternative has had its turn.
    let visited: [String]
}

// "Switch to another server" walks the retry order without landing twice on one server per cycle.
enum Rotation {
    static func plan(order: [String], currentId: String, visited: [String]) -> RotationPlan {
        let alternatives = order.filter { $0 != currentId }
        let stillListed = visited.filter { alternatives.contains($0) }
        let fresh = alternatives.filter { !stillListed.contains($0) }
        guard !fresh.isEmpty else { return RotationPlan(plan: alternatives, visited: []) }
        let fallback = alternatives.filter { stillListed.contains($0) }
        return RotationPlan(plan: fresh + fallback, visited: stillListed)
    }

    // The server left, every attempt before the landing, and the landing (or every attempt on failure).
    static func record(visited: [String], currentId: String, plan: [String], landedId: String?) -> [String] {
        let landedAt = landedId.flatMap { plan.firstIndex(of: $0) }
        let attempted = landedAt.map { Array(plan[...$0]) } ?? plan
        return (visited + [currentId] + attempted).reduce(into: [String]()) { unique, id in
            if !unique.contains(id) { unique.append(id) }
        }
    }
}
