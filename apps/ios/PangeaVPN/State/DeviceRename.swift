import Foundation

enum DeviceRename {
    static let maxUnits = 64

    // String.prototype.trim's set: Swift's own adds U+0085 and lacks the byte order mark.
    private static let jsWhitespace: CharacterSet = {
        let nextLine: Unicode.Scalar = "\u{85}"
        let byteOrderMark: Unicode.Scalar = "\u{FEFF}"
        var set = CharacterSet.whitespacesAndNewlines
        set.remove(nextLine)
        set.insert(byteOrderMark)
        return set
    }()

    // The trimmed name, or nil when the hub would refuse it; length is in UTF-16 units, as JavaScript counts.
    static func validated(_ raw: String) -> String? {
        let trimmed = raw.trimmingCharacters(in: jsWhitespace)
        return (1...maxUnits).contains(trimmed.utf16.count) ? trimmed : nil
    }

    static var lengthMessage: String {
        String(localized: "session_rename_length", defaultValue: "Use 1 to 64 characters for the name.")
    }

    static var notFoundMessage: String {
        String(localized: "session_rename_not_found", defaultValue: "This device is no longer on your account.")
    }

    static var unavailableMessage: String {
        String(localized: "session_rename_unavailable", defaultValue: "Renaming devices isn't available right now. Try again later.")
    }

    static var failedMessage: String {
        String(localized: "session_rename_failed", defaultValue: "Failed to rename device. Please try again.")
    }

    // Hub codes ride inside the core's error text; anything unrecognised gets the generic line.
    static func message(for error: Error) -> String {
        let text = (error as? CoreError)?.message ?? error.localizedDescription
        if text.contains("DEVICE_NOT_FOUND") { return notFoundMessage }
        if text.contains("DEVICE_NAMES_UNAVAILABLE") { return unavailableMessage }
        if text.contains("1-64 characters") { return lengthMessage }
        return failedMessage
    }
}
