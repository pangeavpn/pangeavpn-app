import Foundation

enum DeviceLimit {
    static func isDeviceLimit(_ message: String) -> Bool {
        ["device limit", "device_limit"].contains {
            message.range(of: $0, options: .caseInsensitive) != nil
        }
    }
}
