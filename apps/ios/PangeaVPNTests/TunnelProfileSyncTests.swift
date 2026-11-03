import XCTest
@testable import PangeaVPN

@MainActor
final class TunnelProfileSyncTests: XCTestCase {
    func testAllowLanReachesTheSavedProfile() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        await rig.settings.setAllowLan(true)
        await TunnelProfileSync.push(settings: rig.settings, connection: rig.store)
        XCTAssertEqual(rig.tunnel.profiles.last?.allowLan, true)
        await rig.settings.setAllowLan(false)
        await TunnelProfileSync.push(settings: rig.settings, connection: rig.store)
        XCTAssertEqual(rig.tunnel.profiles.map(\.allowLan), [true, false])
    }

    func testUnloadedSettingsAreReadBeforeThePush() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        var stored = AppSettings()
        stored.allowLan = true
        rig.core.configResult = .success(stored)
        XCTAssertFalse(rig.settings.loaded)
        await TunnelProfileSync.push(settings: rig.settings, connection: rig.store)
        XCTAssertTrue(rig.settings.loaded)
        XCTAssertEqual(rig.tunnel.profiles, [rig.settings.tunnelProfile])
        XCTAssertEqual(rig.tunnel.profiles.first?.allowLan, true)
    }

    func testNothingIsPushedWhenSettingsCannotLoad() async throws {
        let (defaults, suite) = try makeTestDefaults()
        defer { defaults.removePersistentDomain(forName: suite) }
        let rig = await makeConnectionRig(defaults)
        rig.core.configResult = .failure(CoreError(message: "core unavailable"))
        await TunnelProfileSync.push(settings: rig.settings, connection: rig.store)
        XCTAssertTrue(rig.tunnel.profiles.isEmpty)
        XCTAssertNotNil(rig.settings.error)
    }
}
