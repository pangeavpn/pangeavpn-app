import SwiftUI

// A dash-grouped code such as an account number, wrapped only between groups and in even
// lines: one line, else halves, else thirds. Left to wrap itself it ran five groups and a
// straggler ("ABCD-EFGH-JKMN-PQRS-TVWX-" / "YZ01"), which reads as two codes.
// Font, tracking and colour come from the caller.
struct GroupedCode: View {
    let code: String

    var body: some View {
        ViewThatFits(in: .horizontal) {
            ForEach(Self.layouts(code), id: \.self) { layout in
                Text(verbatim: layout)
                    .fixedSize()
            }
            Text(verbatim: code)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    // Widest first. Each line ends on its group's hyphen, as when the text wraps itself.
    static func layouts(_ code: String) -> [String] {
        let groups = code.components(separatedBy: "-")
        guard groups.count > 1 else { return [code] }
        var perLine: [Int] = []
        for lines in 1...groups.count {
            let per = (groups.count + lines - 1) / lines
            if !perLine.contains(per) { perLine.append(per) }
        }
        return perLine.map { per in
            stride(from: 0, to: groups.count, by: per)
                .map { groups[$0..<min($0 + per, groups.count)].joined(separator: "-") }
                .joined(separator: "-\n")
        }
    }
}

#if DEBUG
#Preview {
    VStack(alignment: .leading, spacing: 16) {
        GroupedCode(code: "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01").frame(width: 360, alignment: .leading)
        GroupedCode(code: "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01").frame(width: 200, alignment: .leading)
        GroupedCode(code: "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01").frame(width: 120, alignment: .leading)
    }
    .scaledFont(size: 17, design: .monospaced, relativeTo: .body)
    .padding()
}
#endif
