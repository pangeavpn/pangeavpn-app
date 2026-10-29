import SwiftUI

enum SVGPath {
    static func parse(_ d: String) -> Path {
        var path = Path()
        var command: Character?
        var numbers: [CGFloat] = []
        let separators = CharacterSet.whitespacesAndNewlines.union(CharacterSet(charactersIn: ","))

        for token in d.components(separatedBy: separators) where !token.isEmpty {
            if let value = Double(token), value.isFinite {
                numbers.append(CGFloat(value))
                if numbers.count == 2, let cmd = command {
                    apply(cmd, numbers[0], numbers[1], to: &path)
                    command = cmd == "M" ? "L" : cmd
                    numbers.removeAll()
                }
                continue
            }
            numbers.removeAll()
            command = nil
            guard token.count == 1, let letter = token.first else { continue }
            switch letter {
            case "M", "L": command = letter
            case "Z": path.closeSubpath()
            default: break
            }
        }
        return path
    }

    private static func apply(_ command: Character, _ x: CGFloat, _ y: CGFloat, to path: inout Path) {
        let point = CGPoint(x: x, y: y)
        if command == "M" {
            path.move(to: point)
        } else if path.isEmpty {
            path.move(to: point)
        } else {
            path.addLine(to: point)
        }
    }
}
