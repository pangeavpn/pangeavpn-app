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

    private static func isAllowedSymbol(_ s: Unicode.Scalar) -> Bool {
        ("0"..."9").contains(s) || ("A"..."Z").contains(s) || ("a"..."z").contains(s)
    }
}
