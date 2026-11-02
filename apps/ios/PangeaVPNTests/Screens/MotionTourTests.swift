import SwiftUI
import UIKit
import XCTest
@testable import PangeaVPN

// Walks the live screens through every state change while CI records the simulator, so motion
// can be reviewed frame by frame off-Mac. Skipped unless PANGEA_MOTION_DIR is set.
@MainActor
final class MotionTourTests: XCTestCase {
    private var stepLog: [String] = []
    private var window: UIWindow?

    private func directory() throws -> URL {
        guard let path = ScreenGallery.environment["PANGEA_MOTION_DIR"], !path.isEmpty else {
            throw XCTSkip("PANGEA_MOTION_DIR is unset, so the motion tour is not recorded.")
        }
        let url = URL(fileURLWithPath: path, isDirectory: true)
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }

    // Each step is stamped in epoch seconds, so its frames can be cut from the recording.
    private func step(_ name: String, hold seconds: Double) async throws {
        stepLog.append(String(format: "%.3f %@", Date().timeIntervalSince1970, name))
        try await Task.sleep(for: .seconds(seconds))
    }

    private func show<V: View>(_ view: V) throws {
        let scenes = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }
        guard let scene = scenes.first(where: { $0.activationState == .foregroundActive }) ?? scenes.first else {
            throw XCTSkip("the test host has no window scene")
        }
        window?.isHidden = true
        let next = UIWindow(windowScene: scene)
        next.windowLevel = UIWindow.Level(rawValue: UIWindow.Level.alert.rawValue + 1)
        next.rootViewController = UIHostingController(rootView: view)
        next.makeKeyAndVisible()
        window = next
    }

    func testTour() async throws {
        let root = try directory()
        defer {
            window?.isHidden = true
            let device = ScreenGallery.deviceFolder
            try? stepLog.joined(separator: "\n").write(
                to: root.appendingPathComponent("steps-\(device).txt"), atomically: true, encoding: .utf8)
        }
        try await launch()
        try await mainScreen()
    }

    // Splash cross-fades into the main screen once the session restores.
    private func launch() async throws {
        let stores = try GalleryStores(subscription: GalleryData.active)
        try show(stores.stage(RootView()))
        try await step("launch", hold: 3)
    }

    private func mainScreen() async throws {
        let stores = try GalleryStores(subscription: GalleryData.active)
        await stores.load()
        try show(stores.stage(MainView()))
        try await step("idle", hold: 2.5)

        stores.tunnel.status = GalleryData.status(.connecting)
        try await step("connecting", hold: 2.5)
        stores.tunnel.connectedDate = Date()
        stores.tunnel.status = GalleryData.status(.connected, bytesIn: 1_572_864, bytesOut: 327_680,
                                                  transport: "reality", postQuantum: true)
        try await step("connected", hold: 3)

        if let other = stores.servers.regions.first(where: { $0.key != stores.servers.selectedRegionKey }) {
            await stores.connection.activate(region: other)
        }
        try await step("switched", hold: 3)

        stores.connection.showServerSheet = true
        try await step("picker-open", hold: 2.5)
        stores.connection.showServerSheet = false
        try await step("picker-closed", hold: 2.5)

        stores.tunnel.status = GalleryData.status(.disconnecting)
        try await step("disconnecting", hold: 2)
        stores.tunnel.connectedDate = nil
        stores.tunnel.status = .disconnected
        try await step("disconnected", hold: 3)

        stores.tunnel.status = GalleryData.status(.error, detail: "The server did not answer the handshake.")
        try await step("error", hold: 3)

        stores.core.subscriptionResult = .success(GalleryData.pastDue)
        _ = await stores.session.refreshSubscription()
        try await step("banner", hold: 3)
    }
}
