import XCTest
@testable import PangeaVPN

private func server(_ id: String) -> Server {
    Server(id: id, name: id, region: "eu", country: "GB", load: nil)
}

private func settings(lastServerId: String) -> AppSettings {
    var settings = AppSettings()
    settings.lastServerId = lastServerId
    return settings
}

@MainActor
final class ServersStoreTests: XCTestCase {
    func testSelectPersistsLastServer() async {
        let core = FakeCore()
        let store = ServersStore(core: core)
        await store.select(server("us-1"))
        XCTAssertEqual(store.selected, server("us-1"))
        XCTAssertEqual(core.savedSettings.last?.lastServerId, "us-1")
        XCTAssertNil(store.error)
    }

    func testSelectKeepsThePickWhenSavingFails() async {
        let core = FakeCore()
        core.setConfigResult = .failure(CoreError(message: "mobile: keychain unavailable"))
        let store = ServersStore(core: core)
        await store.select(server("us-1"))
        XCTAssertEqual(store.selected, server("us-1"))
        XCTAssertEqual(store.error, "mobile: keychain unavailable")
    }

    func testSelectKeepsTheOtherStoredSettings() async {
        let core = FakeCore()
        var current = AppSettings()
        current.allowLan = true
        current.mtu = 1280
        core.configResult = .success(current)
        let store = ServersStore(core: core)
        await store.select(server("us-1"))
        XCTAssertEqual(core.savedSettings.last?.allowLan, true)
        XCTAssertEqual(core.savedSettings.last?.mtu, 1280)
    }

    func testRefreshGroupsRegionsAndFallsBackToLastServer() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1"), server("us-1"), server("eu-2")])
        core.configResult = .success(settings(lastServerId: "us-1"))
        let store = ServersStore(core: core)
        await store.refresh()
        XCTAssertEqual(store.regions.map(\.key), ["eu", "us"])
        XCTAssertEqual(store.selected?.id, "us-1")
        XCTAssertEqual(store.selectedRegionKey, "us")
        XCTAssertFalse(store.loading)
    }

    func testRefreshKeepsTheCurrentPick() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1"), server("us-1")])
        core.configResult = .success(settings(lastServerId: "eu-1"))
        let store = ServersStore(core: core)
        await store.select(server("us-1"))
        await store.refresh()
        XCTAssertEqual(store.selected?.id, "us-1")
    }

    func testRefreshClearsAPickThatIsGone() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1")])
        core.configResult = .success(settings(lastServerId: "us-1"))
        let store = ServersStore(core: core)
        await store.select(server("ap-1"))
        await store.refresh()
        XCTAssertNil(store.selected)
    }

    func testRefreshFailureShowsErrorAndKeepsRegions() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1")])
        let store = ServersStore(core: core)
        await store.refresh()
        core.serversResult = .failure(CoreError(message: "mobile: hub unreachable"))
        await store.refresh()
        XCTAssertEqual(store.regions.map(\.key), ["eu"])
        XCTAssertEqual(store.error, "mobile: hub unreachable")
    }

    func testSlotsKeepTheSelectedRegion() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1"), server("us-1"), server("ap-1")])
        let store = ServersStore(core: core)
        await store.refresh()
        XCTAssertEqual(store.slots.map(\.key), ["eu", "us"])
        await store.select(server("ap-1"))
        XCTAssertEqual(store.slots.map(\.key), ["ap", "eu"])
    }
}
