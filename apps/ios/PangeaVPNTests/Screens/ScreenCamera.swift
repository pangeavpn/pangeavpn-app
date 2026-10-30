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

    // Cleared after the first unanswered request, so a dead watcher costs one timeout.
    static var framebuffer = environment["PANGEA_SCREENS_FRAMEBUFFER"] == "1"

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

    private static let tick = Duration.milliseconds(100)
    private static let readyTicks = 100
    private static let stillTicks = 30
    // A busy 3-core runner can take well over 10 s to answer one screenshot.
    private static let framebufferTicks = 300

    private let folder: URL

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

    private func capture<V: View>(_ view: V, shot: Shot, ready: @MainActor () -> Bool) async throws {
        guard let scene = Self.activeScene() else { throw Failure.noScene }
        let previousKey = scene.keyWindow
        // The scene override restyles the app's own window too, so the status bar follows the look.
        scene.traitOverrides.userInterfaceStyle = shot.look.style
        let window = UIWindow(windowScene: scene)
        window.windowLevel = UIWindow.Level(rawValue: UIWindow.Level.alert.rawValue + 1)
        window.overrideUserInterfaceStyle = shot.look.style
        window.traitOverrides.preferredContentSizeCategory = shot.look.textSize
        let host = UIHostingController(rootView: view)
        window.rootViewController = host
        window.makeKeyAndVisible()
        let outcome: Result<Void, Error>
        do {
            try await settle(host, shot: shot, ready: ready)
            if shot.scrollToEnd { try await scrollToEnd(host) }
            let png = try await stillFrame(of: window, file: shot.file)
            try await save(png, as: shot.file)
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

    private func scrollToEnd(_ host: UIViewController) async throws {
        let top = host.presentedViewController ?? host
        guard let scroll = Self.tallestScrollView(in: top.view) else { return }
        // Lazy stacks grow as rows appear, so the end is measured again after each jump.
        for _ in 0..<3 {
            let end = scroll.contentSize.height - scroll.bounds.height + scroll.adjustedContentInset.bottom
            let offset = CGPoint(x: scroll.contentOffset.x, y: max(end, -scroll.adjustedContentInset.top))
            scroll.setContentOffset(offset, animated: false)
            try await Task.sleep(for: Self.tick)
        }
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

    private func save(_ png: Data, as file: String) async throws {
        let target = folder.appendingPathComponent("\(file).png")
        if try await framebufferCaptured(to: target, file: file) { return }
        try png.write(to: target, options: .atomic)
    }

    // The workflow's watcher answers each request with `simctl io screenshot`, adding the
    // status bar and home indicator, which live outside the app and so outside any render.
    private func framebufferCaptured(to target: URL, file: String) async throws -> Bool {
        guard ScreenGallery.framebuffer, let root = ScreenGallery.directory,
              let udid = ScreenGallery.environment["SIMULATOR_UDID"] else { return false }
        let files = FileManager.default
        let queue = root.appendingPathComponent(".requests/\(udid)", isDirectory: true)
        if files.fileExists(atPath: target.path) { try files.removeItem(at: target) }
        try files.createDirectory(at: queue, withIntermediateDirectories: true)
        try Data(target.path.utf8).write(to: queue.appendingPathComponent("\(file).want"), options: .atomic)
        for _ in 0..<Self.framebufferTicks {
            try await Task.sleep(for: Self.tick)
            if files.fileExists(atPath: target.path) { return true }
        }
        ScreenGallery.framebuffer = false
        XCTFail("no framebuffer capture of \(file) arrived; app-window renders are kept from here on")
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
