import Foundation

// Where wrapped UI text may break. Every locale joins its summaries with " · ", so the
// rules apply at display time rather than in each translation.
enum LineBreaks {
    static let noBreakSpace = "\u{00A0}"
    // Zero-width; forbids a break on either side of it.
    static let wordJoiner = "\u{2060}"

    // "a · b" wraps as "a ·" / "b", never with a line starting on the dot.
    static func separators(_ text: String) -> String {
        text.replacingOccurrences(of: " · ", with: noBreakSpace + "· ")
    }

    // A server id like "eu-central-2" wraps whole, never at its hyphens.
    static func ids(_ text: String) -> String {
        text.replacingOccurrences(of: "-", with: "-" + wordJoiner)
    }

    // A date like "Oct 20, 2026" stays on one line.
    static func unbroken(_ text: String) -> String {
        text.replacingOccurrences(of: " ", with: noBreakSpace)
    }

    // The part of a summary before its first separator: "eu-london-1 · best of 2" gives "eu-london-1".
    static func lead(_ text: String) -> String {
        text.components(separatedBy: " · ").first ?? text
    }
}
