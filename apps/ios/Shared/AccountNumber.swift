import Foundation

enum AccountNumber {
    private static let groupSize = 4

    // ASCII only, so Arabic-Indic digits are not mistaken for a legacy token.
    private static func isLegacyToken(_ value: String) -> Bool {
        value.unicodeScalars.allSatisfy { ("0"..."9").contains($0) }
    }

    private static func stripped(_ value: String) -> String {
        String(value.filter { !$0.isWhitespace && $0 != "-" }).uppercased()
    }

    static func normalize(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if isLegacyToken(trimmed) { return trimmed }
        return String(stripped(trimmed).map { ch -> Character in
            switch ch {
            case "O": return "0"
            case "I", "L": return "1"
            default: return ch
            }
        })
    }

    static func format(_ raw: String) -> String {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if isLegacyToken(trimmed) { return trimmed }
        let chars = Array(stripped(trimmed))
        return stride(from: 0, to: chars.count, by: groupSize)
            .map { String(chars[$0..<min($0 + groupSize, chars.count)]) }
            .joined(separator: "-")
    }
}
