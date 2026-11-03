import Foundation

// Go core calls block on network I/O, so stores never make them on the main actor.
enum CoreQueue {
    // Config edits read then write, so they run one at a time or an edit could be lost.
    static let settings = DispatchQueue(label: "org.pangeavpn.app.settings")

    static func run<T: Sendable>(
        on queue: DispatchQueue = .global(qos: .userInitiated),
        _ work: @escaping @Sendable () throws -> T
    ) async throws -> T {
        try await withCheckedThrowingContinuation { continuation in
            queue.async { continuation.resume(with: Result { try work() }) }
        }
    }

    static func perform<T: Sendable>(
        on queue: DispatchQueue = .global(qos: .userInitiated),
        _ work: @escaping @Sendable () -> T
    ) async -> T {
        await withCheckedContinuation { continuation in
            queue.async { continuation.resume(returning: work()) }
        }
    }
}

extension CoreBridge {
    // Edits the core's current config, never a copy that another store may have outdated.
    func editConfig(_ change: (inout AppSettings) -> Void) throws -> AppSettings {
        var next = try config()
        change(&next)
        return try setConfig(next)
    }
}
