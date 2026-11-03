import Foundation

enum TokenInput {
    static let maxSymbols = 64

    static func sanitize(_ raw: String) -> String {
        var symbols = 0
        var out = String.UnicodeScalarView()
        for scalar in raw.unicodeScalars {
            let isDash = scalar == "-"
            guard isDash || isAllowedSymbol(scalar) else { continue }
            if !isDash {
                guard symbols < maxSymbols else { break }
                symbols += 1
            }
            out.append(scalar)
        }
        return String(out)
    }

    // A wrapping field types Return as a newline; a paste that happens to end in one is not a submit.
    static func isReturnPress(old: String, new: String) -> Bool {
        let withoutNewlines = new.filter { !$0.isNewline }
        return withoutNewlines != new && withoutNewlines == old
    }

    private static func isAllowedSymbol(_ s: Unicode.Scalar) -> Bool {
        ("0"..."9").contains(s) || ("A"..."Z").contains(s) || ("a"..."z").contains(s)
    }
}
