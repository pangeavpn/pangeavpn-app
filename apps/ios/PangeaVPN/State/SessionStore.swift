import Foundation
import Observation
import os

@MainActor
@Observable
final class SessionStore {
    private(set) var session: Session?
    private(set) var subscription: Subscription?
    private(set) var restoring = false
    private(set) var loggingIn = false
    var loginError: String?
    var showDeviceLimit = false
    private(set) var devices: [Device] = []
    private(set) var loadingDevices = false
    var deviceError: String?

    private let core: any CoreBridge
    private let tunnel: any TunnelControlling
    // Memory only, for the device-limit retry; never persisted.
    @ObservationIgnored private var pendingToken: String?

    private static let log = Logger(subsystem: "org.pangeavpn", category: "session")

    init(core: any CoreBridge, tunnel: any TunnelControlling) {
        self.core = core
        self.tunnel = tunnel
    }

    func restore() async {
        restoring = true
        let restored = await restoredSession()
        session = restored
        restoring = false
        if restored != nil { await loadSubscription() }
    }

    func login(_ raw: String) async {
        let token = AccountNumber.normalize(raw)
        guard !token.isEmpty else {
            loginError = String(localized: "login_empty_account", defaultValue: "Enter your account number")
            return
        }
        pendingToken = token
        await attemptLogin(token)
    }

    func retryLogin() async {
        guard let token = pendingToken else { return }
        await attemptLogin(token)
    }

    func signOut() async {
        await tunnel.disconnect()
        let core = self.core
        await CoreQueue.perform { core.logout() }
        session = nil
        subscription = nil
        devices = []
        showDeviceLimit = false
        loginError = nil
        deviceError = nil
        pendingToken = nil
    }

    func loadSubscription() async {
        let core = self.core
        do {
            subscription = try await CoreQueue.run { try core.subscription() }
        } catch {
            Self.log.error("subscription failed: \(error.localizedDescription, privacy: .public)")
        }
    }

    func loadDevices() async {
        loadingDevices = true
        deviceError = nil
        let core = self.core
        do {
            devices = try await CoreQueue.run { try core.listDevices() }
        } catch {
            deviceError = error.localizedDescription
        }
        loadingDevices = false
    }

    func removeDevice(_ id: String) async {
        deviceError = nil
        let core = self.core
        do {
            try await CoreQueue.run { try core.removeDevice(id: id) }
        } catch {
            deviceError = error.localizedDescription
            return
        }
        await loadDevices()
    }

    private func restoredSession() async -> Session? {
        let core = self.core
        do {
            return try await CoreQueue.run { try core.restoreSession() }
        } catch let error as CoreError where Self.isSignedOut(error) {
            Self.log.info("no stored session")
            return nil
        } catch {
            Self.log.error("restore failed: \(error.localizedDescription, privacy: .public)")
            return nil
        }
    }

    private static func isSignedOut(_ error: CoreError) -> Bool {
        (TunnelStart.mapRestoreError(error) as? TunnelStartError) == .signedOut
    }

    private func attemptLogin(_ token: String) async {
        loggingIn = true
        loginError = nil
        let core = self.core
        do {
            let signedIn = try await CoreQueue.run { try core.login(token: token) }
            loggingIn = false
            pendingToken = nil
            showDeviceLimit = false
            session = signedIn
            await loadSubscription()
        } catch {
            loggingIn = false
            showLoginFailure(error.localizedDescription)
        }
    }

    private func showLoginFailure(_ message: String) {
        if DeviceLimit.isDeviceLimit(message) {
            showDeviceLimit = true
        } else {
            loginError = message
        }
    }
}
