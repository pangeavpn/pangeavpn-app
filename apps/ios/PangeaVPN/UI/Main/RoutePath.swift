import SwiftUI

struct RouteNode: Equatable {
    let name: String
    let country: String

    // The tunnel's own name for a node the list no longer carries, then its id as a last resort.
    static func resolve(id: String, fallbackName: String, in regions: [Region]) -> RouteNode {
        let servers = regions.flatMap { $0.servers }
        if let server = servers.first(where: { $0.id == id }) {
            return RouteNode(name: server.name.isEmpty ? server.id : server.name, country: server.country)
        }
        return RouteNode(name: fallbackName.isEmpty ? id : fallbackName, country: "")
    }
}

// You, then the entry, then the exit; names drop to flags alone when the row is tight.
struct RoutePath: View {
    let entry: RouteNode
    let exit: RouteNode
    let live: Bool

    init(entry: RouteNode, exit: RouteNode, live: Bool) {
        self.entry = entry
        self.exit = exit
        self.live = live
    }

    private var spoken: String {
        String(format: String(localized: "main_route_label", defaultValue: "Multihop route: you, then %1$@, then %2$@"),
               entry.name, exit.name)
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            strip(named: true)
            strip(named: false)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
    }

    private func strip(named: Bool) -> some View {
        HStack(spacing: 0) {
            Image(systemName: "iphone")
                .scaledFont(size: 11, weight: .semibold, relativeTo: .caption)
                .foregroundStyle(Palette.textSecondary)
                .padding(.horizontal, 7)
                .padding(.vertical, 4)
                .background(Palette.surface, in: Capsule())
                .overlay(Capsule().strokeBorder(Palette.border, lineWidth: 1))
            RouteLink(live: live, offset: 0)
            RouteStop(node: entry, named: named, ring: live ? Palette.accent.opacity(0.7) : Palette.border, glow: false)
            RouteLink(live: live, offset: 0.5)
            RouteStop(node: exit, named: named, ring: live ? Palette.connected : Palette.border, glow: live)
        }
        .padding(3)
        .background(
            LinearGradient(colors: [Palette.accent.opacity(0.14), Palette.accent.opacity(0.03)],
                           startPoint: .topLeading, endPoint: .bottomTrailing),
            in: Capsule())
        .overlay(Capsule().strokeBorder(Palette.accent.opacity(0.28), lineWidth: 1))
        .fixedSize()
    }
}

private struct RouteStop: View {
    let node: RouteNode
    let named: Bool
    let ring: Color
    let glow: Bool

    var body: some View {
        HStack(spacing: 5) {
            Text(Flag.emoji(node.country))
                .scaledFont(size: 13, relativeTo: .caption)
            if named {
                Text(node.name)
                    .scaledFont(size: 12, weight: .semibold, relativeTo: .caption)
                    .foregroundStyle(Palette.text)
                    .lineLimit(1)
            }
        }
        .padding(.leading, 6)
        .padding(.trailing, named ? 9 : 6)
        .padding(.vertical, 3)
        .background(Palette.surface, in: Capsule())
        .overlay(Capsule().strokeBorder(ring, lineWidth: 1))
        .overlay(Capsule().strokeBorder(Palette.connected.opacity(glow ? 0.22 : 0), lineWidth: 2).padding(-2))
    }
}

// One packet per cycle crosses the first link, then the second, as on desktop.
private struct RouteLink: View {
    let live: Bool
    let offset: Double

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private static let cycle = 3.2
    private static let width: CGFloat = 22

    init(live: Bool, offset: Double) {
        self.live = live
        self.offset = offset
    }

    var body: some View {
        TimelineView(.animation(paused: !live || reduceMotion)) { context in
            Canvas { gc, size in
                drawLink(gc, size: size, live: live,
                         pulse: live && !reduceMotion ? pulse(at: context.date) : nil)
            }
        }
        .frame(width: Self.width, height: 8)
        .flipsForRightToLeftLayoutDirection(true)
        .accessibilityHidden(true)
    }

    private func pulse(at date: Date) -> Double? {
        let phase = date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: Self.cycle) / Self.cycle
        let local = (phase - offset + 1).truncatingRemainder(dividingBy: 1)
        return local < 0.5 ? local / 0.5 : nil
    }
}

private func drawLink(_ gc: GraphicsContext, size: CGSize, live: Bool, pulse: Double?) {
    let dash = live ? Palette.accent.opacity(0.9) : Palette.textMuted.opacity(0.55)
    let mid = size.height / 2
    var x: CGFloat = 0
    while x < size.width {
        gc.fill(Path(CGRect(x: x, y: mid - 1, width: 3.3, height: 2)), with: .color(dash))
        x += 6
    }
    guard let pulse else { return }
    let fade = min(1, min(pulse, 1 - pulse) / 0.15)
    let dot: CGFloat = 5
    let left = CGFloat(pulse) * (size.width - dot)
    let glow = CGRect(x: left - 2, y: mid - dot / 2 - 2, width: dot + 4, height: dot + 4)
    gc.fill(Path(ellipseIn: glow), with: .color(Palette.accent.opacity(0.3 * fade)))
    gc.fill(Path(ellipseIn: CGRect(x: left, y: mid - dot / 2, width: dot, height: dot)),
            with: .color(Palette.accent.opacity(fade)))
}

#if DEBUG
#Preview {
    VStack(alignment: .leading, spacing: 16) {
        RoutePath(entry: RouteNode(name: "Amsterdam", country: "NL"),
                  exit: RouteNode(name: "New York", country: "US"), live: true)
        RoutePath(entry: RouteNode(name: "Frankfurt", country: "DE"),
                  exit: RouteNode(name: "Singapore", country: "SG"), live: false)
        RoutePath(entry: RouteNode(name: "A very long entry region name", country: "GB"),
                  exit: RouteNode(name: "Another very long exit region", country: "JP"), live: true)
            .frame(width: 280)
    }
    .padding(20)
    .background(Palette.bg)
}
#endif
