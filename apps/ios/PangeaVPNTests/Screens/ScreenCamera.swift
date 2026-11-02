import SwiftUI
import UIKit
import XCTest

@MainActor
enum ScreenGallery {
    static let environment = ProcessInfo.processInfo.environment

    // CI sets TEST_RUNNER_PANGEA_SCREENS_DIR; xcodebuild strips the prefix for the test host.
    static var directory: URL? {
        guard let path = environment["PANGEA_SCREENS_DIR"], !path.isEmpty else { return nil }
        return URL(fileURLWithPath: path, isDirectory: true)
    }

    // Cleared after a few unanswered requests in a row, so a dead watcher stops costing timeouts.
    static var framebuffer = environment["PANGEA_SCREENS_FRAMEBUFFER"] == "1"
    static var missedCaptures = 0
    static let missesBeforeGivingUp = 3

    static var deviceFolder: String {
        let name = environment["SIMULATOR_DEVICE_NAME"] ?? UIDevice.current.name
        return name.components(separatedBy: CharacterSet.alphanumerics.inverted)
            .filter { !$0.isEmpty }
            .joined(separator: "-")
    }
}

// Draws through the render server in a window on the host app's scene, so insets, scale and
// Liquid Glass are the simulator's own; layer.render(in:) misdraws those on iOS 17 and later.
@MainActor
final class ScreenCamera {
    struct Look {
        let suffix: String
        let style: UIUserInterfaceStyle
        let textSize: UIContentSizeCategory

        static let light = Look(suffix: "light", style: .light, textSize: .large)
        static let dark = Look(suffix: "dark", style: .dark, textSize: .large)
        static let largeText = Look(suffix: "light-ax3", style: .light, textSize: .accessibilityExtraExtraLarge)
        static let standard = [Look.light, Look.dark]
        static let all = [Look.light, Look.dark, Look.largeText]
    }

    enum Failure: Error, CustomStringConvertible {
        case noScene
        case notReady(String)
        case noImage(String)

        var description: String {
            switch self {
            case .noScene: return "the test host has no window scene to draw in"
            case .notReady(let file): return "\(file) never finished loading or presenting"
            case .noImage(let file): return "\(file) could not be drawn"
            }
        }
    }

    private struct Shot {
        let file: String
        let look: Look
        let presents: Bool
        let scrollToEnd: Bool
    }

    // Sideways is the fallback when iPadOS keeps the scene upright: drawn turned, then turned back.
    private enum Orientation {
        case upright, turned
        case sideways(UIEdgeInsets)
    }

    private static let tick = Duration.milliseconds(100)
    private static let readyTicks = 100
    private static let stillTicks = 30
    // A busy 3-core runner can take well over 10 s to answer one screenshot.
    private static let framebufferTicks = 300
    private static let turnTicks = 30
    // The rotation animation outlasts the geometry change that starts it.
    private static let turnSettle = Duration.milliseconds(600)

    private let folder: URL
    private var orientation = Orientation.upright

    init(root: URL) throws {
        folder = root.appendingPathComponent(ScreenGallery.deviceFolder, isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    }

    func shoot<V: View>(
        _ name: String,
        looks: [Look] = Look.standard,
        presents: Bool = false,
        scrollToEnd: Bool = false,
        before: @MainActor () -> Void = {},
        ready: @MainActor () -> Bool = { true },
        @ViewBuilder content: @MainActor () -> V
    ) async throws {
        UIView.setAnimationsEnabled(false)
        defer { UIView.setAnimationsEnabled(true) }
        for look in looks {
            before()
            let shot = Shot(file: "\(name)-\(look.suffix)", look: look, presents: presents, scrollToEnd: scrollToEnd)
            try await capture(content(), shot: shot, ready: ready)
        }
    }

    // Shoots everything in `shots` in landscape, then turns the scene back upright.
    func inLandscape(_ shots: @MainActor () async throws -> Void) async throws {
        guard let scene = Self.activeScene() else { throw Failure.noScene }
        let turned = try await Self.turn(scene, landscape: true)
        if turned {
            orientation = .turned
        } else {
            print("::notice::iPadOS kept the scene upright, so landscape shots are drawn sideways and turned back")
            _ = try await Self.turn(scene, landscape: false)
            orientation = .sideways(scene.keyWindow?.safeAreaInsets ?? .zero)
        }
        let outcome: Result<Void, Error>
        do {
            try await shots()
            outcome = .success(())
        } catch {
            outcome = .failure(error)
        }
        orientation = .upright
        if turned {
            let upright = try await Self.turn(scene, landscape: false)
            if !upright { XCTFail("the scene never turned back to portrait, so later shots may be sideways") }
        }
        try outcome.get()
    }

    private static func turn(_ scene: UIWindowScene, landscape: Bool) async throws -> Bool {
        let mask: UIInterfaceOrientationMask = landscape ? .landscapeRight : .portrait
        scene.requestGeometryUpdate(UIWindowScene.GeometryPreferences.iOS(interfaceOrientations: mask)) { error in
            print("::notice::the scene declined to turn: \(error.localizedDescription)")
        }
        for _ in 0..<turnTicks {
            try await Task.sleep(for: tick)
            let bounds = scene.coordinateSpace.bounds
            if scene.effectiveGeometry.interfaceOrientation.isLandscape == landscape,
               (bounds.width > bounds.height) == landscape {
                try await Task.sleep(for: turnSettle)
                return true
            }
        }
        return false
    }

    private func capture<V: View>(_ view: V, shot: Shot, ready: @MainActor () -> Bool) async throws {
        guard let scene = Self.activeScene() else { throw Failure.noScene }
        let previousKey = scene.keyWindow
        // The scene override restyles the app's own window too, so the status bar follows the look.
        scene.traitOverrides.userInterfaceStyle = shot.look.style
        let window = UIWindow(windowScene: scene)
        window.windowLevel = UIWindow.Level(rawValue: UIWindow.Level.alert.rawValue + 1)
        window.overrideUserInterfaceStyle = shot.look.style
        window.traitOverrides.preferredContentSizeCategory = shot.look.textSize
        var sidewaysInsets: UIEdgeInsets?
        if case .sideways(let insets) = orientation { sidewaysInsets = insets }
        let host: UIViewController
        if let sidewaysInsets {
            host = Self.sideways(view, in: window, insets: sidewaysInsets)
        } else {
            host = UIHostingController(rootView: view)
        }
        window.rootViewController = host
        window.makeKeyAndVisible()
        let outcome: Result<Void, Error>
        do {
            try await settle(host, shot: shot, ready: ready)
            // A screen that fits has no end to show, and its top shot already covers it.
            var worthSaving = true
            if shot.scrollToEnd { worthSaving = try await scrollToEnd(host) }
            if worthSaving {
                let png = try await stillFrame(of: window, file: shot.file)
                if sidewaysInsets != nil {
                    try Self.uprighted(png, file: shot.file).write(to: pngURL(shot.file), options: .atomic)
                } else {
                    let landscape = window.bounds.width > window.bounds.height
                    try await save(png, as: shot.file, landscape: landscape)
                }
            }
            outcome = .success(())
        } catch {
            outcome = .failure(error)
        }
        await dismantle(window, host: host, scene: scene, previousKey: previousKey)
        try outcome.get()
    }

    private func settle(_ host: UIViewController, shot: Shot, ready: @MainActor () -> Bool) async throws {
        for _ in 0..<Self.readyTicks {
            try await Task.sleep(for: Self.tick)
            let presented = !shot.presents || host.presentedViewController != nil
            if presented && ready() && Self.isSettled(host) { return }
        }
        throw Failure.notReady(shot.file)
    }

    // Lays the screen out landscape, turned a quarter clockwise to fill a portrait window, with landscape's safe area.
    private static func sideways<V: View>(_ view: V, in window: UIWindow, insets: UIEdgeInsets) -> UIViewController {
        let portrait = window.bounds.size
        let safeArea = EdgeInsets(top: insets.top, leading: 0, bottom: insets.bottom, trailing: 0)
        let screen = UIHostingController(rootView: view.safeAreaPadding(safeArea))
        screen.safeAreaRegions = []
        let container = UIViewController()
        container.addChild(screen)
        screen.view.autoresizingMask = []
        screen.view.bounds = CGRect(x: 0, y: 0, width: portrait.height, height: portrait.width)
        screen.view.center = CGPoint(x: portrait.width / 2, y: portrait.height / 2)
        screen.view.transform = CGAffineTransform(rotationAngle: .pi / 2)
        container.view.addSubview(screen.view)
        screen.didMove(toParent: container)
        return container
    }

    // Undoes the sideways layout's clockwise quarter turn.
    private static func uprighted(_ png: Data, file: String) throws -> Data {
        guard let image = UIImage(data: png)?.cgImage else { throw Failure.noImage(file) }
        let width = CGFloat(image.width)
        let height = CGFloat(image.height)
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.opaque = true
        format.preferredRange = .standard
        let renderer = UIGraphicsImageRenderer(size: CGSize(width: height, height: width), format: format)
        return renderer.pngData { context in
            context.cgContext.translateBy(x: 0, y: width)
            context.cgContext.rotate(by: -.pi / 2)
            UIImage(cgImage: image).draw(in: CGRect(x: 0, y: 0, width: width, height: height))
        }
    }

    // Walks children and presentations, so a push inside a NavigationStack or a sheet counts too.
    private static func isSettled(_ root: UIViewController) -> Bool {
        var pending = [root]
        var seen = Set<ObjectIdentifier>()
        while let controller = pending.popLast() {
            guard seen.insert(ObjectIdentifier(controller)).inserted else { continue }
            if controller.isBeingPresented || controller.transitionCoordinator != nil { return false }
            pending += controller.children
            if let presented = controller.presentedViewController { pending.append(presented) }
        }
        return true
    }

    private func scrollToEnd(_ host: UIViewController) async throws -> Bool {
        let top = host.presentedViewController ?? host
        guard let scroll = Self.tallestScrollView(in: top.view) else { return false }
        // Lazy stacks grow as rows appear, so the end is measured again after each jump.
        for _ in 0..<3 {
            let end = scroll.contentSize.height - scroll.bounds.height + scroll.adjustedContentInset.bottom
            let offset = CGPoint(x: scroll.contentOffset.x, y: max(end, -scroll.adjustedContentInset.top))
            scroll.setContentOffset(offset, animated: false)
            try await Task.sleep(for: Self.tick)
        }
        return true
    }

    private static func tallestScrollView(in root: UIView) -> UIScrollView? {
        var found: UIScrollView?
        var pending = [root]
        while let view = pending.popLast() {
            if let scroll = view as? UIScrollView, scroll.contentSize.height > scroll.bounds.height,
               scroll.bounds.height > (found?.bounds.height ?? 0) {
                found = scroll
            }
            pending += view.subviews
        }
        return found
    }

    private func stillFrame(of window: UIWindow, file: String) async throws -> Data {
        var previous: Data?
        for _ in 0..<Self.stillTicks {
            let frame = Self.render(window)
            if let frame, frame == previous { return frame }
            previous = frame ?? previous
            try await Task.sleep(for: Self.tick)
        }
        guard let previous else { throw Failure.noImage(file) }
        XCTFail("\(file) never held still, so its last frame was kept")
        return previous
    }

    private static func render(_ window: UIWindow) -> Data? {
        let format = UIGraphicsImageRendererFormat(for: window.traitCollection)
        format.opaque = true
        format.preferredRange = .standard
        var drawn = false
        let image = UIGraphicsImageRenderer(bounds: window.bounds, format: format).image { _ in
            drawn = window.drawHierarchy(in: window.bounds, afterScreenUpdates: true)
        }
        return drawn ? image.pngData() : nil
    }

    private func pngURL(_ file: String) -> URL {
        folder.appendingPathComponent("\(file).png")
    }

    private func save(_ png: Data, as file: String, landscape: Bool) async throws {
        let target = pngURL(file)
        if try await framebufferCaptured(to: target, file: file, landscape: landscape) { return }
        try png.write(to: target, options: .atomic)
    }

    // The workflow's watcher answers each request with `simctl io screenshot`, adding the
    // status bar and home indicator, which live outside the app and so outside any render.
    private func framebufferCaptured(to target: URL, file: String, landscape: Bool) async throws -> Bool {
        guard ScreenGallery.framebuffer, let root = ScreenGallery.directory,
              let udid = ScreenGallery.environment["SIMULATOR_UDID"] else { return false }
        let files = FileManager.default
        let queue = root.appendingPathComponent(".requests/\(udid)", isDirectory: true)
        if files.fileExists(atPath: target.path) { try files.removeItem(at: target) }
        // The screen can trail an offscreen render by a frame or two, so it gets a moment to catch up.
        try await Task.sleep(for: .milliseconds(200))
        try files.createDirectory(at: queue, withIntermediateDirectories: true)
        try Data(target.path.utf8).write(to: queue.appendingPathComponent("\(file).want"), options: .atomic)
        for _ in 0..<Self.framebufferTicks {
            try await Task.sleep(for: Self.tick)
            if files.fileExists(atPath: target.path) {
                ScreenGallery.missedCaptures = 0
                return Self.keepsOrientation(target, file: file, landscape: landscape)
            }
        }
        // One slow capture only costs that shot its status bar, so it warns rather than fails.
        ScreenGallery.missedCaptures += 1
        print("::warning::no framebuffer capture of \(file) arrived, so its app-window render is kept")
        if ScreenGallery.missedCaptures >= ScreenGallery.missesBeforeGivingUp {
            ScreenGallery.framebuffer = false
            XCTFail("\(ScreenGallery.missedCaptures) framebuffer captures in a row never arrived; the watcher looks dead")
        }
        return false
    }

    // simctl may return a turned scene in the device's own portrait, which the app-window render then replaces.
    private static func keepsOrientation(_ target: URL, file: String, landscape: Bool) -> Bool {
        guard landscape, let size = UIImage(contentsOfFile: target.path)?.size else { return true }
        if size.width > size.height { return true }
        print("::notice::\(file) came back from simctl in portrait, so its app-window render is kept")
        return false
    }

    private func dismantle(_ window: UIWindow, host: UIViewController, scene: UIWindowScene, previousKey: UIWindow?) async {
        if host.presentedViewController != nil {
            host.dismiss(animated: false)
            try? await Task.sleep(for: Self.tick)
        }
        window.isHidden = true
        window.rootViewController = nil
        scene.traitOverrides.remove(UITraitUserInterfaceStyle.self)
        previousKey?.makeKey()
    }

    private static func activeScene() -> UIWindowScene? {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        return scenes.first { $0.activationState == .foregroundActive } ?? scenes.first
    }
}
