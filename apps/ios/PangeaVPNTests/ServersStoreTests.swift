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

    func testOverlappingRefreshesListOnce() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1")])
        let store = ServersStore(core: core)
        async let first: Void = store.refresh()
        async let second: Void = store.refresh()
        _ = await (first, second)
        XCTAssertEqual(core.calls.filter { $0 == "listServers" }.count, 1)
        XCTAssertEqual(store.regions.map(\.key), ["eu"])
        XCTAssertFalse(store.loading)
    }

    func testResetClearsTheListPickAndError() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1"), server("us-1")])
        let store = ServersStore(core: core)
        await store.refresh()
        await store.select(server("us-1"))
        store.error = "stale"
        store.reset()
        XCTAssertTrue(store.regions.isEmpty)
        XCTAssertTrue(store.slots.isEmpty)
        XCTAssertNil(store.selected)
        XCTAssertNil(store.error)
    }

    func testResetDropsARefreshStillInFlight() async {
        let core = FakeCore()
        core.serversResult = .success([server("eu-1")])
        core.configDelay = 0.2
        let store = ServersStore(core: core)
        async let refreshing: Void = store.refresh()
        var spins = 0
        while !store.loading && spins < 1000 {
            await Task.yield()
            spins += 1
        }
        XCTAssertTrue(store.loading)
        store.reset()
        await refreshing
        XCTAssertTrue(store.regions.isEmpty)
        XCTAssertNil(store.selected)
        XCTAssertFalse(store.loading)
    }
}
