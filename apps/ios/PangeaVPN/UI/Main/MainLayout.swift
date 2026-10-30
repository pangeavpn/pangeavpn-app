import SwiftUI

struct MapSlot: LayoutValueKey {
    static let defaultValue = false
}

enum MapSizing {
    // Small enough that the fullest state still fits a 17 Pro without scrolling.
    static let minimum: CGFloat = 80
    static let maximum: CGFloat = 288

    // The map gives way first, so the rest fits before the screen has to scroll.
    static func side(viewport: CGFloat, content: CGFloat, width: CGFloat,
                     minimum: CGFloat = MapSizing.minimum, maximum: CGFloat = MapSizing.maximum) -> CGFloat {
        let room = viewport - content
        let wanted = min(max(room, minimum), maximum)
        return max(0, min(wanted, width))
    }

    // Spare height goes around the map, so a tall screen keeps the hero below it, not floating.
    static func band(viewport: CGFloat, content: CGFloat, side: CGFloat) -> CGFloat {
        max(side, viewport - content)
    }
}

// Stacks its children top to bottom at their own heights; the MapSlot child gets what is left.
struct MainLayout: Layout {
    let viewportHeight: CGFloat

    init(viewportHeight: CGFloat) {
        self.viewportHeight = viewportHeight
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.replacingUnspecifiedDimensions().width
        let plan = MainLayoutPlan(width: width, viewport: viewportHeight, subviews: subviews)
        return CGSize(width: width, height: plan.total)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let plan = MainLayoutPlan(width: bounds.width, viewport: viewportHeight, subviews: subviews)
        var y = bounds.minY
        for index in subviews.indices {
            let subview = subviews[index]
            if subview[MapSlot.self] {
                let side = plan.mapSide
                let top = y + (plan.mapBand - side) / 2
                subview.place(at: CGPoint(x: bounds.midX, y: top), anchor: .top,
                              proposal: ProposedViewSize(width: side, height: side))
                y += plan.mapBand
            } else {
                let height = plan.heights[index]
                subview.place(at: CGPoint(x: bounds.minX, y: y), anchor: .topLeading,
                              proposal: ProposedViewSize(width: bounds.width, height: height))
                y += height
            }
        }
    }
}

private struct MainLayoutPlan {
    let heights: [CGFloat]
    let mapSide: CGFloat
    let mapBand: CGFloat
    let total: CGFloat

    init(width: CGFloat, viewport: CGFloat, subviews: LayoutSubviews) {
        let measured: [CGFloat] = subviews.map { subview in
            if subview[MapSlot.self] { return 0 }
            return subview.sizeThatFits(ProposedViewSize(width: width, height: nil)).height
        }
        let content = measured.reduce(0, +)
        let hasMap = subviews.contains { $0[MapSlot.self] }
        let side = hasMap ? MapSizing.side(viewport: viewport, content: content, width: width) : 0
        let band = hasMap ? MapSizing.band(viewport: viewport, content: content, side: side) : 0
        heights = measured
        mapSide = side
        mapBand = band
        total = content + band
    }
}
