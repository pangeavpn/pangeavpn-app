import Foundation
import XCTest

// A throwaway suite, so tests never touch the shared App Group defaults.
func makeTestDefaults() throws -> (defaults: UserDefaults, suite: String) {
    let suite = UUID().uuidString
    return (try XCTUnwrap(UserDefaults(suiteName: suite)), suite)
}
