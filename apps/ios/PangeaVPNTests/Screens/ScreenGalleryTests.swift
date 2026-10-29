import SwiftUI
import XCTest
@testable import PangeaVPN

// Writes every screen to PANGEA_SCREENS_DIR/<device>/ for review off-Mac; skipped when it is unset,
// so the ordinary test run never pays for it. A new screen is one more `shoot` call.
@MainActor
final class ScreenGalleryTests: XCTestCase {
    private func gallery(
        status: TunnelStatus = .disconnected,
        connectedFor: TimeInterval? = nil,
        subscription: Subscription? = GalleryData.active,
        servers: [Server] = GalleryData.servers
    ) async throws -> (camera: ScreenCamera, stores: GalleryStores) {
        guard let root = ScreenGallery.directory else {
            throw XCTSkip("PANGEA_SCREENS_DIR is unset, so the screen gallery is not rendered.")
        }
        let stores = try GalleryStores(
            status: status, connectedFor: connectedFor, subscription: subscription, servers: servers)
        await stores.load()
        let camera = try ScreenCamera(root: root)
        return (camera, stores)
    }

    func testSplash() async throws {
        let (camera, _) = try await gallery()
        try await camera.shoot("01-splash") { SplashView() }
    }

    func testLogin() async throws {
        let (camera, stores) = try await gallery()
        try await camera.shoot("02-login", looks: ScreenCamera.Look.all) {
            stores.stage(LoginView(token: .constant("")))
        }
        stores.session.loginError = "Invalid account number"
        try await camera.shoot("03-login-error") {
            stores.stage(LoginView(token: .constant("ABCD-EFGH-JKMN-PQRS-TVWX-YZ01")))
        }
    }

    func testDeviceLimit() async throws {
        let (camera, stores) = try await gallery()
        stores.session.showDeviceLimit = true
        await stores.session.loadDevices()
        try await camera.shoot("04-device-limit", ready: { !stores.session.loadingDevices }) {
            stores.stage(DeviceLimitView())
        }
        stores.session.loginError = "Network unreachable"
        try await camera.shoot("05-device-limit-error", ready: { !stores.session.loadingDevices }) {
            stores.stage(DeviceLimitView())
        }
    }

    func testMain() async throws {
        let connected = GalleryData.status(.connected, bytesIn: 1_572_864, bytesOut: 327_680, transport: "reality")
        let failed = GalleryData.status(.error, detail: "The server did not answer the handshake.")
        let states: [(name: String, status: TunnelStatus, seconds: TimeInterval?, plan: Subscription?)] = [
            ("06-main-disconnected", .disconnected, nil, nil),
            ("07-main-connecting", GalleryData.status(.connecting), nil, GalleryData.active),
            ("08-main-connected", connected, 754, GalleryData.active),
            ("09-main-error", failed, nil, GalleryData.pastDue),
        ]
        for state in states {
            let (camera, stores) = try await gallery(
                status: state.status, connectedFor: state.seconds, subscription: state.plan)
            try await camera.shoot(state.name, looks: ScreenCamera.Look.all,
                                   before: { stores.restartClock() },
                                   ready: { !stores.servers.regions.isEmpty }) {
                stores.stage(MainView())
            }
            try await camera.shoot("\(state.name)-end", looks: [.light, .largeText], scrollToEnd: true,
                                   before: { stores.restartClock() },
                                   ready: { !stores.servers.regions.isEmpty }) {
                stores.stage(MainView())
            }
        }
    }

    // Dismissing a sheet writes false back through its binding, so each look opens it again.
    func testServerSheet() async throws {
        let (camera, stores) = try await gallery()
        try await camera.shoot("10-servers", presents: true,
                               before: { stores.connection.showServerSheet = true },
                               ready: { !stores.servers.regions.isEmpty }) {
            stores.stage(MainView())
        }
    }

    func testServerSheetEmpty() async throws {
        let (camera, stores) = try await gallery(servers: [])
        try await camera.shoot("11-servers-empty", presents: true,
                               before: { stores.connection.showServerSheet = true },
                               ready: { !stores.servers.loading }) {
            stores.stage(MainView())
        }
    }

    // MainView keeps its settings flag private, so the sheet is presented from outside it.
    func testSettings() async throws {
        let (camera, stores) = try await gallery()
        try await camera.shoot("12-settings", looks: ScreenCamera.Look.all, presents: true) {
            stores.stage(MainView().sheet(isPresented: .constant(true)) { SettingsSheet() })
        }
        try await camera.shoot("12-settings-end", presents: true, scrollToEnd: true) {
            stores.stage(MainView().sheet(isPresented: .constant(true)) { SettingsSheet() })
        }
        stores.settings.error = "At least one connection method must stay on."
        try await camera.shoot("13-settings-error", presents: true) {
            stores.stage(MainView().sheet(isPresented: .constant(true)) { SettingsSheet() })
        }
    }
}
