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
        let connected = GalleryData.status(.connected, bytesIn: 1_572_864, bytesOut: 327_680,
                                           transport: "reality", postQuantum: true)
        let multihop = GalleryData.status(.connected, bytesIn: 88_431_002, bytesOut: 4_120_000,
                                          transport: "hysteria2", postQuantum: true,
                                          entry: ("nl-ams-1", "Amsterdam"))
        let failed = GalleryData.status(.error, detail: "The server did not answer the handshake.")
        let states: [(name: String, status: TunnelStatus, seconds: TimeInterval?, plan: Subscription?)] = [
            ("06-main-disconnected", .disconnected, nil, GalleryData.active),
            ("07-main-connecting", GalleryData.status(.connecting), nil, GalleryData.active),
            ("08-main-connected", connected, 754, GalleryData.active),
            ("08b-main-multihop", multihop, 3_725, GalleryData.active),
            ("09-main-error", failed, nil, GalleryData.pastDue),
            ("09b-main-expired", .disconnected, nil, GalleryData.expired),
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

    func testMainWithAnotherAccent() async throws {
        let (camera, stores) = try await gallery(status: GalleryData.status(.disconnected))
        // Restored here rather than in tearDown, whose override would differ in actor isolation.
        defer { AccentTheme.shared.accent = .terra }
        stores.settings.setAccent(.ocean)
        try await camera.shoot("06b-main-ocean", ready: { !stores.servers.regions.isEmpty }) {
            stores.stage(MainView())
        }
    }

    func testRegionPicker() async throws {
        let (camera, stores) = try await gallery(status: GalleryData.status(.connected, transport: "reality"))
        try await camera.shoot("10-regions", looks: ScreenCamera.Look.all, ready: { !stores.servers.regions.isEmpty }) {
            stores.stage(RegionPickerView())
        }
        try await camera.shoot("10-regions-end", looks: [.light], scrollToEnd: true,
                               ready: { !stores.servers.regions.isEmpty }) {
            stores.stage(RegionPickerView())
        }
        stores.settings.setMultihopEnabled(true)
        try await camera.shoot("10b-regions-multihop", ready: { !stores.servers.regions.isEmpty }) {
            stores.stage(RegionPickerView())
        }
    }

    func testRegionPickerEmpty() async throws {
        let (camera, stores) = try await gallery(servers: [])
        try await camera.shoot("11-regions-empty", ready: { !stores.servers.loading }) {
            stores.stage(RegionPickerView())
        }
    }

    func testSettingsHome() async throws {
        let (camera, stores) = try await gallery()
        try await camera.shoot("12-settings", looks: ScreenCamera.Look.all) {
            stores.stage(SettingsView())
        }
        try await camera.shoot("12-settings-end", looks: [.light, .dark], scrollToEnd: true) {
            stores.stage(SettingsView())
        }
        stores.settings.setDeveloperMode(true)
        try await camera.shoot("12b-settings-developer", scrollToEnd: true) {
            stores.stage(SettingsView())
        }
    }

    // Every page opens already pushed, so each one is its own shot.
    func testSettingsPages() async throws {
        let (camera, stores) = try await gallery(status: GalleryData.status(.connected, transport: "reality"))
        stores.settings.setDeveloperMode(true)
        await stores.session.loadDevices()
        let pages: [SettingsRoute] = [
            .transport, .provisioning, .network, .killSwitch, .splitTunnel, .startup,
            .notifications, .panic, .language, .appearance, .support, .update, .developer, .devices,
        ]
        for (index, route) in pages.enumerated() {
            let name = String(format: "13-%02d-%@", index + 1, String(describing: route))
            try await camera.shoot(name, looks: [.light, .dark],
                                   ready: { !stores.session.loadingDevices }) {
                stores.stage(SettingsView(path: [route]))
            }
        }
        // Set on appear, since tearing down the last shot clears a page's error on its way out.
        let error = "At least one connection method must stay on."
        try await camera.shoot("13-99-settings-error", ready: { stores.settings.error == error }) {
            stores.stage(SettingsView(path: [.provisioning]))
                .onAppear { stores.settings.error = error }
        }
    }

    func testSettingsPagesAtLargeText() async throws {
        let (camera, stores) = try await gallery()
        for route in [SettingsRoute.network, .killSwitch, .devices] {
            try await camera.shoot("14-large-\(route)", looks: [.largeText], scrollToEnd: true) {
                stores.stage(SettingsView(path: [route]))
            }
        }
    }
}
