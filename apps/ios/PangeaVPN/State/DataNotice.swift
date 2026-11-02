import Foundation

// Whether this device has seen the data notice; raising the version shows it again after a change.
enum DataNotice {
    static let version = 1
    static let key = "privacy.dataNoticeVersion"

    static func isAccepted(in defaults: UserDefaults) -> Bool {
        defaults.integer(forKey: key) >= version
    }

    static func accept(in defaults: UserDefaults) {
        defaults.set(version, forKey: key)
    }
}
