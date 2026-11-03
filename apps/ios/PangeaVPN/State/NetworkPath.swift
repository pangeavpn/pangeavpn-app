import Foundation
import Network

// After the VPN profile goes, iOS rebuilds its routes, and a hub call made meanwhile fails at once.
enum NetworkPath {
    static func waitUntilUsable(within timeout: TimeInterval) async {
        guard timeout > 0 else { return }
        let monitor = NWPathMonitor()
        let queue = DispatchQueue(label: "org.pangeavpn.network-path")
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            let finish = RunOnce {
                monitor.cancel()
                continuation.resume()
            }
            monitor.pathUpdateHandler = { path in
                if usable(path) { finish.run() }
            }
            monitor.start(queue: queue)
            queue.asyncAfter(deadline: .now() + timeout) { finish.run() }
        }
    }

    // A path still through a tunnel is the old profile on its way out.
    static func usable(_ path: NWPath) -> Bool {
        path.status == .satisfied && !path.usesInterfaceType(.other)
    }
}

private final class RunOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var action: (() -> Void)?

    init(_ action: @escaping () -> Void) {
        self.action = action
    }

    func run() {
        let pending: (() -> Void)? = lock.withLock {
            let current = action
            action = nil
            return current
        }
        pending?()
    }
}
