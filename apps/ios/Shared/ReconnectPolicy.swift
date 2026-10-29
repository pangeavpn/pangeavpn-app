import Foundation

struct ReconnectPolicy {
    enum Decision: Equatable {
        case ignore
        case reconnect(after: TimeInterval)
        case waitForNetwork
    }

    let debounce: TimeInterval = 1.5
    let maxAttempts = 3

    private var lastKey: String?
    private var waiting = false

    func backoff(attempt: Int) -> TimeInterval {
        TimeInterval(3 * attempt)
    }

    mutating func pathChanged(key: String, usable: Bool) -> Decision {
        guard usable else {
            waiting = true
            return .waitForNetwork
        }
        let previous = lastKey
        lastKey = key
        if waiting {
            waiting = false
            return .reconnect(after: debounce)
        }
        guard let previous, previous != key else { return .ignore }
        return .reconnect(after: debounce)
    }
}
