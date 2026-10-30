import Foundation
import Observation
import os

struct SessionTimeouts: Sendable {
    let deregister: TimeInterval
    let coreWipe: TimeInterval

    // The hub search alone can outlast 8 s when censored, and sign-out must not hang on it.
    // A panic wipe must finish even if the core is stuck; the local cleanup runs either way.
    init(deregister: TimeInterval = 8, coreWipe: TimeInterval = 3) {
        self.deregister = deregister
        self.coreWipe = coreWipe
    }

    static let standard = SessionTimeouts()
}

@MainActor
@Observable
final class SessionStore {
    private(set) var session: Session?
    private(set) var subscription: Subscription?
    private(set) var restoring = false
    private(set) var loggingIn = false
    private(set) var signingOut = false
    var loginError: String?
    var showDeviceLimit = false
    private(set) var devices: [Device] = []
    private(set) var loadingDevices = false
    var deviceError: String?

    // Only an explicit "not entitled" blocks connecting; an unknown answer never does.
    var entitled: Bool { subscription?.entitled != false }

    private let core: any CoreBridge
    private let tunnel: any TunnelControlling
    private let authenticator: any DeviceAuthenticating
    private let wiper: PanicWiper
    private let timeouts: SessionTimeouts
    // Memory only, for the device-limit retry; never persisted.
    @ObservationIgnored private var pendingToken: String?
    @ObservationIgnored private var clientInfoSent = false
    // Bumped when the session clears, so a lookup still in flight for the old account is dropped.
    @ObservationIgnored private var generation = 0

    private static let log = Logger(subsystem: "org.pangeavpn", category: "session")

    init(
        core: any CoreBridge,
        tunnel: any TunnelControlling,
        authenticator: any DeviceAuthenticating = DeviceOwnerAuthenticator(),
        wiper: PanicWiper = .live(),
        timeouts: SessionTimeouts = .standard
    ) {
        self.core = core
        self.tunnel = tunnel
        self.authenticator = authenticator
        self.wiper = wiper
        self.timeouts = timeouts
    }

    func restore() async {
        restoring = true
        await sendClientInfoOnce()
        let outcome = await restoreOutcome()
        session = outcome.session
        restoring = false
        switch outcome {
        case .restored:
            await loadSubscription()
        case .signedOut:
            // A profile left behind would let Connect On Demand start a tunnel that fails signed out.
            await tunnel.removeConfiguration()
        case .failed:
            break
        }
    }

    // A second tap while one login is in flight would register the device twice.
    func login(_ raw: String) async {
        guard !loggingIn else { return }
        let token = AccountNumber.normalize(raw)
        guard !token.isEmpty else {
            loginError = String(localized: "login_empty_account", defaultValue: "Enter your account number")
            return
        }
        pendingToken = token
        await attemptLogin(token)
    }

    func retryLogin() async {
        guard !loggingIn, let token = pendingToken else { return }
        await attemptLogin(token)
    }

    // The profile goes before the license, so nothing can start a tunnel that would fail signed out.
    func signOut() async {
        guard !signingOut else { return }
        signingOut = true
        let holdsSlot = session != nil
        await tunnel.disconnect()
        await tunnel.removeConfiguration()
        // Cancelling on the device-limit screen has no registered slot to free.
        if holdsSlot { await deregister() }
        let core = self.core
        await CoreQueue.perform { core.logout() }
        clearSession()
        signingOut = false
    }

    // Panic: local only, because speed and leaving nothing behind matter more than freeing the slot.
    func wipe() async {
        await tunnel.disconnect()
        await tunnel.removeConfiguration()
        let core = self.core
        let outcome = await BoundedCall.run(timeout: timeouts.coreWipe) { try core.wipe() }
        report("core wipe", outcome)
        wiper.run()
        clearSession()
    }

    func loadSubscription() async {
        _ = await fetchSubscription()
    }

    // False when no answer came back: the hub refused, or was unreachable with nothing cached.
    @discardableResult
    func refreshSubscription() async -> Bool {
        await fetchSubscription()
    }

    func loadDevices() async {
        loadingDevices = true
        deviceError = nil
        let started = generation
        let core = self.core
        do {
            let listed = try await CoreQueue.run { try core.listDevices() }
            if started == generation { devices = listed }
        } catch {
            if started == generation { deviceError = error.localizedDescription }
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

    func renameDevice(_ id: String, to name: String) async -> Bool {
        deviceError = nil
        guard let trimmed = DeviceRename.validated(name) else {
            deviceError = DeviceRename.lengthMessage
            return false
        }
        let core = self.core
        do {
            let stored = try await CoreQueue.run { try core.renameDevice(id: id, name: trimmed) }
            let shown = stored.isEmpty ? trimmed : stored
            devices = devices.map { $0.id == id ? $0.renamed(shown) : $0 }
            return true
        } catch {
            Self.log.error("rename failed (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            deviceError = DeviceRename.message(for: error)
            return false
        }
    }

    // The account number is the sign-in credential, so the phone's owner must confirm first.
    func revealAccountNumber() async -> String? {
        let reason = String(localized: "session_reveal_reason", defaultValue: "Confirm it's you to see your account number.")
        let outcome = await authenticator.authenticate(reason: reason)
        guard outcome != .denied else { return nil }
        let core = self.core
        let number = await CoreQueue.perform { core.accountNumber() }
        let trimmed = number.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    private func sendClientInfoOnce() async {
        guard !clientInfoSent else { return }
        clientInfoSent = true
        let info = ClientInfo.current()
        let core = self.core
        await CoreQueue.perform { core.setClientInfo(appVersion: info.app, osVersion: info.system) }
    }

    private func fetchSubscription() async -> Bool {
        let started = generation
        let core = self.core
        do {
            let answer = try await CoreQueue.run { try core.subscription() }
            guard started == generation else { return false }
            subscription = answer
            return true
        } catch {
            Self.log.error("subscription failed (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
            return false
        }
    }

    // Best effort: a failure or timeout keeps the identity, so signing back in reuses the slot.
    private func deregister() async {
        let core = self.core
        let outcome = await BoundedCall.run(timeout: timeouts.deregister) { try core.deregister() }
        report("deregister", outcome)
    }

    private func report(_ step: String, _ outcome: BoundedCall.Outcome) {
        switch outcome {
        case .finished:
            return
        case .timedOut:
            Self.log.info("\(step, privacy: .public) timed out, carrying on")
        case .failed(let code, let message):
            Self.log.error("\(step, privacy: .public) failed (\(code, privacy: .public)): \(message, privacy: .private)")
        }
    }

    private func clearSession() {
        generation += 1
        session = nil
        subscription = nil
        devices = []
        showDeviceLimit = false
        loginError = nil
        deviceError = nil
        pendingToken = nil
    }

    private func restoreOutcome() async -> RestoreOutcome {
        let core = self.core
        do {
            let restored = try await CoreQueue.run { try core.restoreSession() }
            return .restored(restored)
        } catch let error as CoreError where Self.isSignedOut(error) {
            Self.log.info("no stored session")
            return .signedOut
        } catch {
            Self.log.error("restore failed: \(error.localizedDescription, privacy: .private)")
            return .failed
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

    // Other errors leave showDeviceLimit alone: a failed retry stays on that screen, which shows loginError.
    private func showLoginFailure(_ message: String) {
        if DeviceLimit.isDeviceLimit(message) {
            showDeviceLimit = true
        } else {
            loginError = message
        }
    }
}

private enum RestoreOutcome {
    case restored(Session)
    // The core holds no session, as opposed to one it could not check just now.
    case signedOut
    case failed

    var session: Session? {
        if case .restored(let session) = self { return session }
        return nil
    }
}

private extension Device {
    func renamed(_ name: String) -> Device {
        Device(id: id, friendlyName: name, createdAt: createdAt, status: status, current: current)
    }
}
