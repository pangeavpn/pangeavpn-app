import XCTest
@testable import PangeaVPN

private func server(_ id: String, load: Int? = nil, transports: Set<String> = Server.universalTransports) -> Server {
    Server(id: id, name: id, region: "eu", country: "GB", load: load, transports: transports)
}

private func settings(lastServerId: String) -> AppSettings {
    var settings = AppSettings()
    settings.lastServerId = lastServerId
    return settings
}

@MainActor
final class ServersStoreTests: XCTestCase {
    private func makeStore(_ core: FakeCore, _ defaults: UserDefaults, list: [Server]? = nil) async -> ServersStore {
        let store = ServersStore(core: core, defaults: defaults)
        if let list {
            core.serversResult = .success(list)
            await store.refresh()
        }
        return store
    }

    func testSelectPersistsLastServer() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults)
        await store.select(server: server("us-1"), pinned: false)
        XCTAssertEqual(store.selected, server("us-1"))
        XCTAssertEqual(core.savedSettings.last?.lastServerId, "us-1")
        XCTAssertNil(store.error)
    }

    func testSelectKeepsThePickWhenSavingFails() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.setConfigResult = .failure(CoreError(message: "mobile: keychain unavailable"))
        let store = await makeStore(core, defaults)
        await store.select(server: server("us-1"), pinned: false)
        XCTAssertEqual(store.selected, server("us-1"))
        XCTAssertEqual(store.error, "mobile: keychain unavailable")
    }

    func testSelectKeepsTheOtherStoredSettings() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        var current = AppSettings()
        current.allowLan = true
        current.mtu = 1280
        core.configResult = .success(current)
        let store = await makeStore(core, defaults)
        await store.select(server: server("us-1"), pinned: false)
        XCTAssertEqual(core.savedSettings.last?.allowLan, true)
        XCTAssertEqual(core.savedSettings.last?.mtu, 1280)
    }

    func testSelectingARegionTakesItsLightestNodeAndDropsThePin() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults, list: [server("eu-1", load: 50), server("eu-2", load: 5)])
        await store.select(server: server("eu-1", load: 50), pinned: true)
        XCTAssertEqual(store.pinnedServerId, "eu-1")
        try await store.select(region: XCTUnwrap(store.regions.first))
        XCTAssertEqual(store.selected?.id, "eu-2")
        XCTAssertNil(store.pinnedServerId)
        XCTAssertNil(defaults.string(forKey: SharedPrefs.Key.pinnedServerId))
        XCTAssertEqual(core.savedSettings.last?.lastServerId, "eu-2")
    }

    func testAPinSurvivesARelaunch() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults)
        await store.select(server: server("eu-1"), pinned: true)
        XCTAssertEqual(ServersStore(core: core, defaults: defaults).pinnedServerId, "eu-1")
    }

    func testRecentsPromoteCapAtEightAndPersist() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults)
        for index in 1...10 {
            await store.select(server: server("r\(index)-1"), pinned: false)
        }
        await store.select(server: server("r7-2"), pinned: false)
        let expected = ["r7", "r10", "r9", "r8", "r6", "r5", "r4", "r3"]
        XCTAssertEqual(store.recentRegionKeys, expected)
        XCTAssertEqual(ServersStore(core: core, defaults: defaults).recentRegionKeys, expected)
    }

    func testRefreshGroupsRegionsAndFallsBackToLastServer() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.configResult = .success(settings(lastServerId: "us-1"))
        let store = await makeStore(core, defaults, list: [server("eu-1"), server("us-1"), server("eu-2")])
        XCTAssertEqual(store.regions.map(\.key), ["eu", "us"])
        XCTAssertEqual(store.selected?.id, "us-1")
        XCTAssertEqual(store.selectedRegionKey, "us")
        XCTAssertFalse(store.loading)
    }

    func testRefreshKeepsTheCurrentPick() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.serversResult = .success([server("eu-1"), server("us-1")])
        core.configResult = .success(settings(lastServerId: "eu-1"))
        let store = await makeStore(core, defaults)
        await store.select(server: server("us-1"), pinned: false)
        core.configResult = .success(settings(lastServerId: "eu-1"))
        await store.refresh()
        XCTAssertEqual(store.selected?.id, "us-1")
    }

    func testRefreshClearsAPickThatIsGone() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.serversResult = .success([server("eu-1")])
        let store = await makeStore(core, defaults)
        await store.select(server: server("ap-1"), pinned: false)
        core.configResult = .success(settings(lastServerId: "us-1"))
        await store.refresh()
        XCTAssertNil(store.selected)
    }

    func testRefreshFailureShowsErrorAndKeepsRegions() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults, list: [server("eu-1")])
        core.serversResult = .failure(CoreError(message: "mobile: hub unreachable"))
        await store.refresh()
        XCTAssertEqual(store.regions.map(\.key), ["eu"])
        XCTAssertEqual(store.error, "mobile: hub unreachable")
    }

    func testSlotsPutRecentsFirstAndKeepTheSelection() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults, list: [server("eu-1"), server("us-1"), server("ap-1")])
        XCTAssertEqual(store.slots.map(\.key), ["eu", "us"])
        await store.select(server: server("ap-1"), pinned: false)
        XCTAssertEqual(store.slots.map(\.key), ["ap", "eu"])
        await store.select(server: server("us-1"), pinned: false)
        XCTAssertEqual(store.slots.map(\.key), ["us", "ap"])
    }

    func testRegionsAndSlotsForATransportHideServersThatCannotCarryIt() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let reality = Server.universalTransports.union(["reality"])
        let list = [server("eu-1"), server("eu-2", transports: reality), server("us-1"), server("ap-1", transports: reality)]
        let store = await makeStore(FakeCore(), defaults, list: list)
        XCTAssertEqual(store.regions(for: "reality").map(\.key), ["eu", "ap"])
        XCTAssertEqual(store.regions(for: "reality").first?.servers.map(\.id), ["eu-2"])
        XCTAssertEqual(store.regions(for: "auto").map(\.key), ["eu", "us", "ap"])
        XCTAssertEqual(store.slots(for: "reality").map(\.key), ["eu", "ap"])
    }

    func testCandidatesFollowTheDesktopRetryOrderAndCap() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let list = [server("eu-1", load: 70), server("us-1", load: 5), server("eu-2", load: 20), server("eu-3"),
                    server("us-2", load: 1), server("ap-1"), server("sa-1"), server("af-1"), server("oc-1")]
        let store = await makeStore(FakeCore(), defaults, list: list)
        XCTAssertTrue(store.candidates(transport: "auto").isEmpty)
        await store.select(server: server("eu-1", load: 70), pinned: false)
        XCTAssertEqual(store.candidates(transport: "auto"), ["eu-1", "eu-2", "eu-3", "us-2", "ap-1", "sa-1"])
        XCTAssertEqual(store.candidates(transport: "cloak"), ["eu-1"])
    }

    func testFollowAdoptsTheLandingAndDropsAStalePin() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults, list: [server("eu-1"), server("eu-2")])
        await store.select(server: server("eu-1"), pinned: true)
        await store.follow(serverId: "eu-1")
        XCTAssertEqual(store.pinnedServerId, "eu-1")
        await store.follow(serverId: "eu-2")
        XCTAssertEqual(store.selected?.id, "eu-2")
        XCTAssertNil(store.pinnedServerId)
        XCTAssertEqual(core.savedSettings.last?.lastServerId, "eu-2")
        XCTAssertEqual(store.recentRegionKeys, ["eu"])
    }

    func testFollowBeforeTheListLoadsWinsTheNextRefresh() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.configResult = .success(settings(lastServerId: "eu-1"))
        let store = await makeStore(core, defaults)
        await store.follow(serverId: "us-1")
        XCTAssertNil(store.selected)
        core.serversResult = .success([server("eu-1"), server("us-1")])
        await store.refresh()
        XCTAssertEqual(store.selected?.id, "us-1")
    }

    func testRotationPlanAndRecordPersist() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults, list: [server("eu-1"), server("eu-2"), server("us-1")])
        XCTAssertTrue(store.rotationPlan(transport: "auto").isEmpty)
        await store.select(server: server("eu-1"), pinned: false)
        let rotation = store.rotation(transport: "auto")
        XCTAssertEqual(store.rotationPlan(transport: "auto").map(\.id), ["eu-2", "us-1"])
        store.recordRotation(rotation, leaving: "eu-1", landedId: "eu-2")
        XCTAssertEqual(store.rotationVisited, ["eu-1", "eu-2"])
        XCTAssertEqual(ServersStore(core: core, defaults: defaults).rotationVisited, ["eu-1", "eu-2"])
    }

    func testOverlappingRefreshesListOnce() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.serversResult = .success([server("eu-1")])
        let store = await makeStore(core, defaults)
        async let first: Void = store.refresh()
        async let second: Void = store.refresh()
        _ = await (first, second)
        XCTAssertEqual(core.calls.filter { $0 == "listServers" }.count, 1)
        XCTAssertEqual(store.regions.map(\.key), ["eu"])
        XCTAssertFalse(store.loading)
    }

    func testResetClearsTheListPickHistoryAndError() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = await makeStore(core, defaults, list: [server("eu-1"), server("us-1")])
        await store.select(server: server("us-1"), pinned: true)
        store.recordRotation(store.rotation(transport: "auto"), leaving: "us-1", landedId: nil)
        store.error = "stale"
        store.reset()
        XCTAssertTrue(store.regions.isEmpty)
        XCTAssertTrue(store.slots.isEmpty)
        XCTAssertNil(store.selected)
        XCTAssertNil(store.error)
        XCTAssertNil(store.pinnedServerId)
        XCTAssertTrue(store.recentRegionKeys.isEmpty)
        XCTAssertTrue(store.rotationVisited.isEmpty)
        let reopened = ServersStore(core: core, defaults: defaults)
        XCTAssertNil(reopened.pinnedServerId)
        XCTAssertTrue(reopened.recentRegionKeys.isEmpty)
        XCTAssertTrue(reopened.rotationVisited.isEmpty)
    }

    func testResetDropsARefreshStillInFlight() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.serversResult = .success([server("eu-1")])
        core.configDelay = 0.2
        let store = await makeStore(core, defaults)
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
