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

    // The task's value if it lands within timeout, else nil; the task runs on either way.
    static func value<T: Sendable>(of task: Task<T, Never>, within timeout: TimeInterval) async -> T? {
        await withCheckedContinuation { (continuation: CheckedContinuation<T?, Never>) in
            let once = ResumeOnce(continuation)
            Task { once.resume(await task.value) }
            DispatchQueue.global(qos: .userInitiated).asyncAfter(deadline: .now() + timeout) { once.resume(nil) }
        }
    }
}

private final class ResumeOnce<Value: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuation: CheckedContinuation<Value, Never>?

    init(_ continuation: CheckedContinuation<Value, Never>) {
        self.continuation = continuation
    }

    func resume(_ outcome: Value) {
        let pending: CheckedContinuation<Value, Never>? = lock.withLock {
            let current = continuation
            continuation = nil
            return current
        }
        pending?.resume(returning: outcome)
    }
}
