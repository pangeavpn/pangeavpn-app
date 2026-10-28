import Foundation

enum DeviceLimit {
    static func isDeviceLimit(_ message: String) -> Bool {
        message.localizedCaseInsensitiveContains("device limit")
            || message.localizedCaseInsensitiveContains("DEVICE_LIMIT")
    }
}
