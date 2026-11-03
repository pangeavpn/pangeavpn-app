import Foundation
import NetworkExtension
import os

// Tracks one NE connection's state and fills in the core's detail by polling the extension.
@MainActor
final class TunnelStatusMonitor {
    private static let pollInterval = Duration.seconds(1)

    private(set) var status = TunnelStatus.disconnected
    private(set) var connectedDate: Date?
    private(set) var state = TunnelState.disconnected
    // Set before an app-side stop so the disconnect it causes is never shown as an error.
    var stopRequested = false
    var onChange: (() -> Void)?
    var onTransition: ((_ previous: TunnelState, _ current: TunnelState) -> Void)?

    private let log = Logger(subsystem: "org.pangeavpn", category: "controller")
    private var connection: NEVPNConnection?
    private var observer: NSObjectProtocol?
    private var pollTask: Task<Void, Never>?

    func watch(_ connection: NEVPNConnection) {
        self.connection = connection
        if let observer { NotificationCenter.default.removeObserver(observer) }
        observer = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange, object: connection, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated { () -> Void in self?.statusChanged() }
        }
        statusChanged()
    }

    func forget() {
        if let observer { NotificationCenter.default.removeObserver(observer) }
        observer = nil
        stopPolling()
        connection = nil
    }

    func reset() {
        state = .disconnected
        stopRequested = false
        connectedDate = nil
        status = .disconnected
    }

    // Stopping an idle tunnel fires no NE transition, so nothing else would consume the flag.
    func settleIdleRelease() {
        stopRequested = false
        guard status.state == .error else { return }
        status = .disconnected
        onChange?()
    }

    private func statusChanged() {
        guard let connection else { return }
        let previous = state
        state = TunnelController.state(for: connection.status)
        connectedDate = state == .connected ? connection.connectedDate : nil
        switch state {
        case .connecting, .connected:
            var next = previous == .disconnected ? TunnelStatus.disconnected : status
            next.state = state
            status = next
            startPolling()
        case .disconnecting:
            stopPolling()
            status.state = .disconnecting
        case .disconnected, .error:
            stopPolling()
            if previous != .disconnected {
                status = .disconnected
                reportDisconnect()
            }
        }
        onTransition?(previous, state)
        onChange?()
    }

    private func reportDisconnect() {
        let requested = stopRequested
        stopRequested = false
        Task { [weak self] in
            guard let connection = self?.connection else { return }
            let error = await ProviderChannel.lastDisconnectError(of: connection)
            self?.showDisconnectError(error, requested: requested)
        }
    }

    private func showDisconnectError(_ error: Error?, requested: Bool) {
        guard let error, !requested, state == .disconnected, !TunnelStart.isStopped(error) else { return }
        var next = TunnelStatus.disconnected
        next.state = .error
        next.detail = error.localizedDescription
        status = next
        onChange?()
    }

    private func startPolling() {
        guard pollTask == nil else { return }
        pollTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                await self.poll()
                do {
                    try await Task.sleep(for: TunnelStatusMonitor.pollInterval)
                } catch {
                    return
                }
            }
        }
    }

    private func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    private func poll() async {
        guard let session = connection as? NETunnelProviderSession else { return }
        do {
            guard let reply = try await ProviderChannel.send(ProviderChannel.stateRequest, on: session) else {
                log.debug("status poll got no reply")
                return
            }
            apply(try JSONDecoder().decode(TunnelStatus.self, from: reply))
        } catch {
            log.debug("status poll failed (\(error.logCode, privacy: .public)): \(error.localizedDescription, privacy: .private)")
        }
    }

    // NE owns the state; the core's status only fills in detail, bytes, transport and server.
    private func apply(_ polled: TunnelStatus) {
        guard state == .connecting || state == .connected else { return }
        var next = polled
        next.state = state
        guard next != status else { return }
        status = next
        onChange?()
    }
}
