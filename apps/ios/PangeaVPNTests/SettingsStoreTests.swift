import XCTest
@testable import PangeaVPN

private let lastMethodError = "at least one hub connection method must stay enabled"

private func stored() -> AppSettings {
    var settings = AppSettings()
    settings.mtu = 1300
    settings.lastServerId = "eu-1"
    return settings
}

@MainActor
final class SettingsStoreTests: XCTestCase {
    func testDisableLastHubMethodKeepsSettingsAndShowsError() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.configResult = .success(stored())
        let store = SettingsStore(core: core, defaults: defaults)
        await store.load()
        core.hubMethodResult = .failure(CoreError(message: lastMethodError))
        await store.setHubMethod("directIp", enabled: false)
        XCTAssertEqual(store.settings, stored())
        XCTAssertEqual(store.error, lastMethodError)
        XCTAssertEqual(core.calls.last, "setHubMethod(directIp,false)")
    }

    func testHubMethodShowsTheStoredValue() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        var next = stored()
        next.hubMethods.directIp = false
        core.hubMethodResult = .success(next)
        let store = SettingsStore(core: core, defaults: defaults)
        await store.setHubMethod("directIp", enabled: false)
        XCTAssertEqual(store.settings, next)
        XCTAssertNil(store.error)
    }

    func testFailedSetConfigKeepsSettingsUntilASuccess() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.configResult = .success(stored())
        let store = SettingsStore(core: core, defaults: defaults)
        await store.load()
        core.setConfigResult = .failure(CoreError(message: "mobile: keychain unavailable"))
        await store.setAllowLan(true)
        XCTAssertEqual(store.settings, stored())
        XCTAssertEqual(store.error, "mobile: keychain unavailable")
        core.setConfigResult = nil
        await store.setAllowLan(true)
        XCTAssertTrue(store.settings.allowLan)
        XCTAssertNil(store.error)
    }

    func testSetterShowsTheStoredValue() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        var corrected = AppSettings()
        corrected.mtu = 1280
        core.setConfigResult = .success(corrected)
        let store = SettingsStore(core: core, defaults: defaults)
        await store.setMtu(9000)
        XCTAssertEqual(core.savedSettings.last?.mtu, 9000)
        XCTAssertEqual(store.settings.mtu, 1280)
    }

    func testEditsApplyToTheCoreConfigNotAStaleCopy() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        await store.load()
        core.configResult = .success(stored())
        await store.setDeadDrop(false)
        XCTAssertEqual(core.savedSettings.last?.lastServerId, "eu-1")
        XCTAssertEqual(core.savedSettings.last?.mtu, 1300)
        XCTAssertEqual(store.settings.deadDrop, false)
    }

    func testCustomDnsSplitsOnCommasAndWhitespace() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        await store.setCustomDns(" 1.1.1.1, 8.8.8.8\t9.9.9.9 ,")
        XCTAssertEqual(store.settings.customDns, ["1.1.1.1", "8.8.8.8", "9.9.9.9"])
    }

    func testTransportStoresItsRawValue() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        await store.setPreferredTransport(.hysteria2)
        XCTAssertEqual(store.settings.preferredTransport, "hysteria2")
    }

    func testKillSwitchAndThemePersistInDefaults() throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        XCTAssertFalse(store.killSwitch)
        XCTAssertNil(store.darkTheme)
        store.setKillSwitch(true)
        store.setDarkTheme(false)
        XCTAssertTrue(defaults.bool(forKey: "killSwitch"))
        let reloaded = SettingsStore(core: core, defaults: defaults)
        XCTAssertTrue(reloaded.killSwitch)
        XCTAssertEqual(reloaded.darkTheme, false)
        reloaded.setDarkTheme(nil)
        XCTAssertNil(reloaded.darkTheme)
        XCTAssertNil(defaults.object(forKey: "darkTheme"))
    }

    func testConcurrentEditsAllLand() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        core.configDelay = 0.02
        let store = SettingsStore(core: core, defaults: defaults)
        async let mtu: Void = store.setMtu(1300)
        async let lan: Void = store.setAllowLan(true)
        async let drop: Void = store.setDeadDrop(false)
        async let transport: Void = store.setPreferredTransport(.reality)
        async let dns: Void = store.setCustomDns("1.1.1.1")
        _ = await (mtu, lan, drop, transport, dns)
        let final = try core.configResult.get()
        XCTAssertEqual(final.mtu, 1300)
        XCTAssertTrue(final.allowLan)
        XCTAssertFalse(final.deadDrop)
        XCTAssertEqual(final.preferredTransport, "reality")
        XCTAssertEqual(final.customDns, ["1.1.1.1"])
        XCTAssertEqual(core.savedSettings.count, 5)
        XCTAssertNil(store.error)
    }

    func testSavedSettingsAreWhatTheNextLoadReads() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let core = FakeCore()
        let store = SettingsStore(core: core, defaults: defaults)
        await store.setAllowLan(true)
        let reloaded = SettingsStore(core: core, defaults: defaults)
        await reloaded.load()
        XCTAssertTrue(reloaded.settings.allowLan)
    }
}
