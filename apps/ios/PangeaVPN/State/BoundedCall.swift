import Foundation

// Races blocking core work against a deadline; after a timeout the work keeps running and its result is dropped.
enum BoundedCall {
    enum Outcome: Equatable, Sendable {
        case finished
        case failed(code: String, message: String)
        case timedOut
    }

    static func run(
        timeout: TimeInterval,
        on queue: DispatchQueue = .global(qos: .userInitiated),
        _ work: @escaping @Sendable () throws -> Void
    ) async -> Outcome {
        await withCheckedContinuation { (continuation: CheckedContinuation<Outcome, Never>) in
            let once = ResumeOnce(continuation)
            queue.async {
                do {
                    try work()
                    once.resume(.finished)
                } catch {
                    once.resume(.failed(code: error.logCode, message: error.localizedDescription))
                }
            }
            queue.asyncAfter(deadline: .now() + timeout) { once.resume(.timedOut) }
        }
    }
}

private final class ResumeOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<BoundedCall.Outcome, Never>?

    init(_ continuation: CheckedContinuation<BoundedCall.Outcome, Never>) {
        self.continuation = continuation
    }

    func resume(_ outcome: BoundedCall.Outcome) {
        let pending: CheckedContinuation<BoundedCall.Outcome, Never>? = lock.withLock {
            let current = continuation
            continuation = nil
            return current
        }
        pending?.resume(returning: outcome)
    }
}
