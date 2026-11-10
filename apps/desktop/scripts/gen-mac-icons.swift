// Generates the committed macOS menu bar and Dock artwork. Dev-time only:
//   swift apps/desktop/scripts/gen-mac-icons.swift
//
// Menu bar: template images (only alpha counts; macOS draws them white on a
// dark menu bar and black on a light one), from the iOS white logo.
// Dock: the iOS accent icons, inset and rounded to the macOS icon grid. The
// Terra one is also the app icon: copy-static.mjs builds the .icns from it.

import AppKit
import CoreGraphics
import Foundation

let scriptDir = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
let desktopDir = scriptDir.deletingLastPathComponent()
let repoDir = desktopDir.deletingLastPathComponent().deletingLastPathComponent()
let assets = repoDir.appendingPathComponent("apps/ios/PangeaVPN/Resources/Assets.xcassets")
let buildDir = desktopDir.appendingPathComponent("build")
let dockDir = buildDir.appendingPathComponent("dock")

func loadImage(_ url: URL) -> CGImage {
    guard let src = CGImageSourceCreateWithURL(url as CFURL, nil),
          let img = CGImageSourceCreateImageAtIndex(src, 0, nil) else {
        fatalError("cannot read \(url.path)")
    }
    return img
}

func context(_ w: Int, _ h: Int) -> CGContext {
    let ctx = CGContext(
        data: nil, width: w, height: h, bitsPerComponent: 8, bytesPerRow: 0,
        space: CGColorSpace(name: CGColorSpace.sRGB)!,
        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
    )!
    ctx.interpolationQuality = .high
    return ctx
}

func write(_ img: CGImage, _ url: URL) {
    let dest = CGImageDestinationCreateWithURL(url as CFURL, "public.png" as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, img, nil)
    guard CGImageDestinationFinalize(dest) else { fatalError("cannot write \(url.path)") }
    print("wrote \(url.path.replacingOccurrences(of: repoDir.path + "/", with: ""))")
}

// The logo's opaque bounds, so the template is trimmed to the shape itself.
func opaqueBounds(_ img: CGImage) -> CGRect {
    let w = img.width, h = img.height
    let ctx = context(w, h)
    ctx.draw(img, in: CGRect(x: 0, y: 0, width: w, height: h))
    let px = ctx.data!.bindMemory(to: UInt8.self, capacity: w * h * 4)
    var minX = w, minY = h, maxX = -1, maxY = -1
    for y in 0..<h {
        for x in 0..<w where px[(y * w + x) * 4 + 3] > 8 {
            minX = min(minX, x); maxX = max(maxX, x)
            minY = min(minY, y); maxY = max(maxY, y)
        }
    }
    // Bitmap rows run top-down; CG coordinates run bottom-up.
    return CGRect(x: minX, y: h - 1 - maxY, width: maxX - minX + 1, height: maxY - minY + 1)
}

// MARK: menu bar

let logo = loadImage(assets.appendingPathComponent("Logo.imageset/logo.png"))
let logoBox = opaqueBounds(logo)
let trimmedLogo = logo.cropping(to: CGRect(
    x: logoBox.minX, y: CGFloat(logo.height) - logoBox.maxY,
    width: logoBox.width, height: logoBox.height
))!

// 18pt tall, the menu bar's usual glyph height; @2x for Retina.
func trayTemplate(scale: Int, connected: Bool) -> CGImage {
    let side = 18 * scale
    let ctx = context(side, side)
    let fit = min(CGFloat(side) / CGFloat(trimmedLogo.width), CGFloat(side) / CGFloat(trimmedLogo.height))
    let lw = CGFloat(trimmedLogo.width) * fit, lh = CGFloat(trimmedLogo.height) * fit
    ctx.draw(trimmedLogo, in: CGRect(x: (CGFloat(side) - lw) / 2, y: (CGFloat(side) - lh) / 2, width: lw, height: lh))
    if connected {
        // A check badge in the bottom-right, with a clear ring cut around it so
        // it reads apart from the continent.
        let s = CGFloat(scale)
        let r: CGFloat = 4.25 * s
        let c = CGPoint(x: CGFloat(side) - r - 0.25 * s, y: r + 0.25 * s)
        ctx.setBlendMode(.clear)
        ctx.fillEllipse(in: CGRect(x: c.x - r - 1.25 * s, y: c.y - r - 1.25 * s, width: 2 * (r + 1.25 * s), height: 2 * (r + 1.25 * s)))
        ctx.setBlendMode(.normal)
        ctx.setFillColor(CGColor(gray: 0, alpha: 1))
        ctx.fillEllipse(in: CGRect(x: c.x - r, y: c.y - r, width: 2 * r, height: 2 * r))
        ctx.setBlendMode(.clear)
        ctx.setLineWidth(1.3 * s)
        ctx.setLineCap(.round)
        ctx.setLineJoin(.round)
        ctx.move(to: CGPoint(x: c.x - 2.0 * s, y: c.y + 0.1 * s))
        ctx.addLine(to: CGPoint(x: c.x - 0.6 * s, y: c.y - 1.4 * s))
        ctx.addLine(to: CGPoint(x: c.x + 2.1 * s, y: c.y + 1.5 * s))
        ctx.strokePath()
    }
    return ctx.makeImage()!
}

for connected in [false, true] {
    let base = connected ? "PangeaVPN_connectedTemplate" : "PangeaVPNTemplate"
    write(trayTemplate(scale: 1, connected: connected), buildDir.appendingPathComponent("\(base).png"))
    write(trayTemplate(scale: 2, connected: connected), buildDir.appendingPathComponent("\(base)@2x.png"))
}

// MARK: Dock

// macOS icon grid: an 824pt body centred on a 1024pt canvas, ~185pt corners,
// with the system's soft drop shadow beneath.
let accents: [(name: String, file: String)] = [
    ("terra", "AppIcon.appiconset/AppIcon.png"),
    ("purple", "AppIcon-Purple.appiconset/AppIcon-Purple.png"),
    ("ocean", "AppIcon-Ocean.appiconset/AppIcon-Ocean.png"),
    ("emerald", "AppIcon-Emerald.appiconset/AppIcon-Emerald.png"),
    ("rose", "AppIcon-Rose.appiconset/AppIcon-Rose.png"),
]

try FileManager.default.createDirectory(at: dockDir, withIntermediateDirectories: true)
for accent in accents {
    let src = loadImage(assets.appendingPathComponent(accent.file))
    let ctx = context(1024, 1024)
    let body = CGRect(x: 100, y: 100, width: 824, height: 824)
    let shape = CGPath(roundedRect: body, cornerWidth: 185, cornerHeight: 185, transform: nil)

    ctx.saveGState()
    ctx.setShadow(offset: CGSize(width: 0, height: -10), blur: 20, color: CGColor(gray: 0, alpha: 0.3))
    ctx.addPath(shape)
    ctx.setFillColor(CGColor(gray: 0, alpha: 1))
    ctx.fillPath()
    ctx.restoreGState()

    ctx.saveGState()
    ctx.addPath(shape)
    ctx.clip()
    ctx.draw(src, in: body)
    ctx.restoreGState()

    write(ctx.makeImage()!, dockDir.appendingPathComponent("PangeaVPN-\(accent.name).png"))
}
