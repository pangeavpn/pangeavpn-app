import Foundation

enum Format {
    static let emDash = "\u{2014}"

    static func duration(seconds: Int) -> String {
        let safe = max(seconds, 0)
        let hours = safe / 3600
        let minutes = (safe % 3600) / 60
        let secs = safe % 60
        if hours > 0 {
            return String(format: "%d:%02d:%02d", hours, minutes, secs)
        }
        return String(format: "%02d:%02d", minutes, secs)
    }

    static func bytes(_ n: Int64) -> String {
        if n < 1024 { return "\(n) B" }
        let units = ["KB", "MB", "GB", "TB"]
        var value = Double(n) / 1024.0
        var index = 0
        while value >= 1024 && index < units.count - 1 {
            value /= 1024
            index += 1
        }
        return String(format: "%.1f %@", value, units[index])
    }

    static func sessionClock(state: TunnelState, connectedDate: Date?, now: Date) -> String {
        guard state == .connected, let date = connectedDate else { return emDash }
        return duration(seconds: Int(now.timeIntervalSince(date)))
    }
}
