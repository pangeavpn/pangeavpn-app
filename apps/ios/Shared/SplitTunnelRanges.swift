import Foundation

// Why the excluded ranges were refused; entries are kept as typed, so the message can name them.
struct SplitTunnelRangesError: Error, Equatable, Sendable {
    let notIPv4: [String]
    let prefixTooShort: [String]
    let tooMany: Bool

    var isEmpty: Bool { notIPv4.isEmpty && prefixTooShort.isEmpty && !tooMany }
}

// Excluded IPv4 ranges for split tunnelling, held to the rules desktop's service applies.
enum SplitTunnelRanges {
    static let maxEntries = 32
    static let minPrefix = 8

    enum Entry: Equatable, Sendable {
        case range(String)
        case notIPv4
        case prefixTooShort

        var value: String? {
            if case .range(let value) = self { return value }
            return nil
        }
    }

    private struct Checked {
        let token: String
        let entry: Entry
    }

    private enum Parsed {
        case network(UInt32, Int)
        case refused(Entry)
    }

    // Empty text clears the list; a range typed twice counts once toward the limit.
    static func parse(_ text: String) -> Result<[String], SplitTunnelRangesError> {
        let checked = tokens(in: text).map { Checked(token: $0, entry: normalize($0)) }
        let ranges = unique(checked.compactMap { $0.entry.value })
        let error = SplitTunnelRangesError(
            notIPv4: unique(checked.filter { $0.entry == .notIPv4 }.map { $0.token }),
            prefixTooShort: unique(checked.filter { $0.entry == .prefixTooShort }.map { $0.token }),
            tooMany: ranges.count > maxEntries
        )
        return error.isEmpty ? .success(ranges) : .failure(error)
    }

    // A bare address becomes a /32 and host bits are cleared, so equal ranges compare equal.
    static func normalize(_ token: String) -> Entry {
        switch parsed(token) {
        case .network(let base, let bits): return .range("\(dotted(base))/\(bits)")
        case .refused(let entry): return entry
        }
    }

    // The tunnel builds its excluded routes with this, so it holds a range to the same rules.
    static func route(_ token: String) -> (address: String, prefix: Int)? {
        guard case .network(let base, let bits) = parsed(token) else { return nil }
        return (dotted(base), bits)
    }

    static func text(_ ranges: [String]) -> String {
        ranges.joined(separator: ", ")
    }

    static func tokens(in text: String) -> [String] {
        text.split(whereSeparator: isSeparator).map(String.init)
    }

    // Persian and Arabic keyboards type their own digits; the addresses need ASCII ones.
    static func foldDigits(_ text: String) -> String {
        String(text.map { char -> Character in
            guard char.unicodeScalars.count == 1,
                  char.unicodeScalars.first?.properties.generalCategory == .decimalNumber,
                  let value = char.wholeNumberValue else { return char }
            return Character(String(value))
        })
    }

    // Arabic, full-width and ideographic commas, as other keyboards type them.
    private static let separators: Set<Character> = [",", ";", "\u{060C}", "\u{FF0C}", "\u{3001}"]

    private static func isSeparator(_ char: Character) -> Bool {
        char.isWhitespace || separators.contains(char)
    }

    private static func parsed(_ token: String) -> Parsed {
        let parts = foldDigits(token).split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count <= 2, let base = address(parts[0]) else { return .refused(.notIPv4) }
        let prefix = parts.count == 2 ? number(parts[1], maxDigits: 2) : 32
        guard let bits = prefix, bits <= 32 else { return .refused(.notIPv4) }
        guard bits >= minPrefix else { return .refused(.prefixTooShort) }
        return .network(base & mask(bits), bits)
    }

    private static func address(_ text: Substring) -> UInt32? {
        let octets = text.split(separator: ".", omittingEmptySubsequences: false)
        guard octets.count == 4 else { return nil }
        let values = octets.compactMap { number($0, maxDigits: 3) }.filter { $0 <= 255 }
        guard values.count == 4 else { return nil }
        return values.reduce(UInt32(0)) { ($0 << 8) | UInt32($1) }
    }

    // Leading zeros are refused, as Go's netip parser refuses them.
    private static func number(_ text: Substring, maxDigits: Int) -> Int? {
        guard !text.isEmpty, text.count <= maxDigits,
              text.allSatisfy({ $0.isASCII && $0.isNumber }),
              text == "0" || text.first != "0" else { return nil }
        return Int(text)
    }

    private static func mask(_ bits: Int) -> UInt32 {
        bits == 0 ? 0 : UInt32.max << (32 - bits)
    }

    private static func dotted(_ value: UInt32) -> String {
        [24, 16, 8, 0].map { String((value >> $0) & 0xFF) }.joined(separator: ".")
    }

    private static func unique(_ items: [String]) -> [String] {
        var seen = Set<String>()
        return items.filter { seen.insert($0).inserted }
    }
}
