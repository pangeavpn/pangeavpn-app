import SwiftUI

private typealias Pt = (CGFloat, CGFloat)

private let viewport: CGFloat = 800
private let orbitSeconds = 90.0
private let hairline = StrokeStyle(lineWidth: 0.75)

private struct Drift {
    let dx: CGFloat
    let dy: CGFloat
    let degrees: Double
}

private struct Plate {
    let outline: Path
    let ridge: Path?
    let stipple: [CGPoint]
    let drift: Drift
}

private func points(_ raw: [Pt]) -> [CGPoint] {
    raw.map { CGPoint(x: $0.0, y: $0.1) }
}

private func ridge(from start: Pt, _ curves: [(Pt, Pt, Pt)]) -> Path {
    var path = Path()
    path.move(to: CGPoint(x: start.0, y: start.1))
    for (c1, c2, end) in curves {
        path.addCurve(
            to: CGPoint(x: end.0, y: end.1),
            control1: CGPoint(x: c1.0, y: c1.1),
            control2: CGPoint(x: c2.0, y: c2.1)
        )
    }
    return path
}

private let plates: [Plate] = [
    Plate(
        outline: SVGPath.parse(DriftPaths.north),
        ridge: ridge(from: (296, 220), [((330, 208), (366, 224), (400, 212)), ((424, 204), (448, 214), (470, 226))]),
        stipple: points([(330, 180), (392, 158), (450, 180), (520, 230), (280, 260), (300, 310), (560, 200)]),
        drift: Drift(dx: 20.8, dy: -88.4, degrees: -3.2)
    ),
    Plate(
        outline: SVGPath.parse(DriftPaths.southWest),
        ridge: ridge(from: (268, 440), [((296, 464), (306, 500), (334, 524)), ((356, 542), (382, 556), (400, 574))]),
        stipple: points([(300, 470), (260, 520), (340, 560), (310, 610), (270, 410)]),
        drift: Drift(dx: -78, dy: 57.2, degrees: -4)
    ),
    Plate(
        outline: SVGPath.parse(DriftPaths.southEast),
        ridge: nil,
        stipple: points([(560, 470), (540, 560), (530, 610), (590, 505)]),
        drift: Drift(dx: 83.2, dy: 67.6, degrees: 4.8)
    ),
]

struct DriftMap: View {
    let state: TunnelState

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var tween: DriftTween

    init(state: TunnelState) {
        self.state = state
        let target = DriftLook.target(for: state)
        _tween = State(initialValue: DriftTween(from: target, to: target, start: .distantPast))
    }

    var body: some View {
        TimelineView(.animation(paused: reduceMotion)) { context in
            Canvas { gc, size in
                let look = tween.look(at: context.date, animated: !reduceMotion)
                let orbit = reduceMotion ? 0 : orbitAngle(at: context.date)
                draw(&gc, size: size, look: look, orbit: orbit)
            }
        }
        .accessibilityHidden(true)
        .onChange(of: state) { _, next in
            let now = Date()
            tween = DriftTween(
                from: tween.look(at: now, animated: !reduceMotion),
                to: DriftLook.target(for: next),
                start: now
            )
        }
    }

    private func orbitAngle(at date: Date) -> Double {
        let phase = date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: orbitSeconds)
        return phase / orbitSeconds * 360
    }

    private func draw(_ gc: inout GraphicsContext, size: CGSize, look: DriftLook, orbit: Double) {
        let unit = min(size.width, size.height) / viewport
        gc.scaleBy(x: unit, y: unit)
        let tint = look.tint
        drawGraticule(gc, tint: tint, alpha: look.alpha)
        drawOrbit(gc, tint: tint, alpha: look.alpha, angle: orbit)
        for plate in plates {
            drawPlate(gc, plate, tint: tint, alpha: look.alpha, spread: look.spread)
        }
        drawCompassRose(gc, tint: tint, alpha: look.alpha)
    }
}

private func oval(rx: CGFloat, ry: CGFloat) -> CGRect {
    CGRect(x: 400 - rx, y: 400 - ry, width: rx * 2, height: ry * 2)
}

private func drawGraticule(_ gc: GraphicsContext, tint: Color, alpha: Double) {
    let paint = tint.opacity(0.16 * alpha)
    gc.stroke(Path(ellipseIn: oval(rx: 340, ry: 340)), with: .color(paint), style: hairline)
    let radii: [(CGFloat, CGFloat)] = [(240, 340), (120, 340), (340, 240), (340, 120)]
    for (rx, ry) in radii {
        gc.stroke(Path(ellipseIn: oval(rx: rx, ry: ry)), with: .color(paint), style: hairline)
    }
    var lines = Path()
    lines.move(to: CGPoint(x: 60, y: 400))
    lines.addLine(to: CGPoint(x: 740, y: 400))
    lines.move(to: CGPoint(x: 400, y: 60))
    lines.addLine(to: CGPoint(x: 400, y: 740))
    gc.stroke(lines, with: .color(paint), style: hairline)
}

private func drawOrbit(_ gc: GraphicsContext, tint: Color, alpha: Double, angle: Double) {
    var rotated = gc
    rotated.translateBy(x: 400, y: 400)
    rotated.rotate(by: .degrees(angle))
    rotated.translateBy(x: -400, y: -400)
    rotated.stroke(
        Path(ellipseIn: oval(rx: 356, ry: 356)),
        with: .color(tint.opacity(0.14 * alpha)),
        style: StrokeStyle(lineWidth: 0.75, dash: [2, 10])
    )
}

private func drawPlate(_ gc: GraphicsContext, _ plate: Plate, tint: Color, alpha: Double, spread: Double) {
    let bounds = plate.outline.boundingRect
    var moved = gc
    moved.translateBy(x: plate.drift.dx * spread, y: plate.drift.dy * spread)
    moved.translateBy(x: bounds.midX, y: bounds.midY)
    moved.rotate(by: .degrees(plate.drift.degrees * spread))
    moved.translateBy(x: -bounds.midX, y: -bounds.midY)

    moved.fill(plate.outline, with: .color(tint.opacity(0.05 * alpha)))
    moved.stroke(
        plate.outline,
        with: .color(tint.opacity(alpha)),
        style: StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round)
    )
    if let ridge = plate.ridge {
        moved.stroke(ridge, with: .color(tint.opacity(0.4 * alpha)), style: StrokeStyle(lineWidth: 1, lineCap: .round))
    }
    for p in plate.stipple {
        let dot = CGRect(x: p.x - 1.6, y: p.y - 1.6, width: 3.2, height: 3.2)
        moved.fill(Path(ellipseIn: dot), with: .color(tint.opacity(0.3 * alpha)))
    }
}

private func drawCompassRose(_ gc: GraphicsContext, tint: Color, alpha: Double) {
    let color = GraphicsContext.Shading.color(tint.opacity(0.5 * alpha))
    let arms: [(Pt, Pt)] = [
        ((676, 138), (676, 198)), ((646, 168), (706, 168)),
        ((657, 149), (695, 187)), ((695, 149), (657, 187)),
    ]
    var cross = Path()
    for (from, to) in arms {
        cross.move(to: CGPoint(x: from.0, y: from.1))
        cross.addLine(to: CGPoint(x: to.0, y: to.1))
    }
    gc.stroke(cross, with: color, style: StrokeStyle(lineWidth: 0.9, lineCap: .round))
    gc.stroke(Path(ellipseIn: CGRect(x: 670, y: 162, width: 12, height: 12)), with: color, style: StrokeStyle(lineWidth: 0.9))
    gc.fill(Path(ellipseIn: CGRect(x: 674.5, y: 166.5, width: 3, height: 3)), with: color)
}
