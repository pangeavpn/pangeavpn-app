import SwiftUI

struct DeviceLimitView: View {
    @Environment(SessionStore.self) private var session
    @Environment(ToastCenter.self) private var toasts
    @State private var loaded = false
    @State private var removedAny = false
    @State private var removingId: String?
    @State private var retrying = false
    @State private var notice: DeviceLimitNotice?

    init() {}

    private var busy: Bool {
        removingId != nil || retrying || session.loggingIn || session.signingOut
    }

    private var listEmpty: Bool {
        loaded && !session.loadingDevices && session.devices.isEmpty && session.deviceError == nil
    }

    private var message: (text: String, isError: Bool)? {
        if let notice { return (notice.text, true) }
        if let raw = session.loginError { return (LoginErrorText.message(for: raw), true) }
        if listEmpty { return (DeviceLimitText.noneLeft, false) }
        return nil
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            SectionLabel(String(localized: "devicelimit_title", defaultValue: "Device Limit Reached"))
            Text(String(
                localized: "devicelimit_subtitle",
                defaultValue: "Your account has reached the maximum number of devices. Remove one to continue signing in."
            ))
            .scaledFont(size: 22, weight: .light, relativeTo: .title2)
            .tracking(-0.5)
            .foregroundStyle(Palette.text)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.top, 8)

            deviceList
                .padding(.top, 22)

            if let message {
                Text(message.text)
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(message.isError ? Palette.error : Palette.textSecondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.vertical, 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            actions
        }
        .padding(.horizontal, 20)
        .padding(.top, 32)
        .padding(.bottom, 24)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Palette.bg.ignoresSafeArea())
        .task { await load() }
    }

    private var actions: some View {
        VStack(spacing: 10) {
            if removedAny || listEmpty {
                PillButton(
                    String(localized: "devicelimit_continue", defaultValue: "Continue Sign In"),
                    enabled: !busy || retrying,
                    loading: retrying,
                    action: continueSignIn
                )
            }
            GhostButton(String(localized: "devicelimit_cancel", defaultValue: "Cancel & Sign Out"), action: cancel)
                .disabled(busy)
                .opacity(session.signingOut ? 0.5 : 1)
        }
        .padding(.top, 6)
    }

    @ViewBuilder
    private var deviceList: some View {
        if session.loadingDevices && session.devices.isEmpty {
            ProgressView()
                .tint(Palette.accent)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ScrollView {
                LazyVStack(spacing: 9) {
                    ForEach(session.devices, id: \.id) { device in
                        DeviceLimitRow(
                            device: device,
                            removing: removingId == device.id,
                            enabled: !busy
                        ) {
                            remove(device)
                        }
                    }
                }
                .padding(.bottom, 12)
            }
            .refreshable { await load() }
        }
    }

    private func load() async {
        await session.loadDevices()
        loaded = true
        if session.deviceError != nil { notice = .loadFailed }
    }

    private func remove(_ device: Device) {
        guard !busy else { return }
        removingId = device.id
        notice = nil
        session.loginError = nil
        Task {
            await session.removeDevice(device.id)
            if session.deviceError == nil {
                removedAny = true
                toasts.show(String(localized: "devicelimit_removed", defaultValue: "Device removed."), style: .success)
            } else {
                notice = .removeFailed
            }
            removingId = nil
        }
    }

    private func continueSignIn() {
        guard !busy else { return }
        retrying = true
        notice = nil
        Task {
            await session.retryLogin()
            let outcome = DeviceLimitText.retryOutcome(
                signedIn: session.session != nil,
                showingLimit: session.showDeviceLimit,
                loginError: session.loginError)
            if outcome == .stillAtLimit {
                notice = .stillAtLimit
                await session.loadDevices()
            }
            retrying = false
        }
    }

    private func cancel() {
        guard !busy else { return }
        notice = nil
        Task { await session.signOut() }
    }
}

enum DeviceLimitNotice: Equatable {
    case loadFailed, removeFailed, stillAtLimit

    var text: String {
        switch self {
        case .loadFailed:
            return String(localized: "devicelimit_load_failed", defaultValue: "Could not load devices. Please try again.")
        case .removeFailed:
            return String(localized: "devicelimit_remove_failed", defaultValue: "Failed to remove device. Please try again.")
        case .stillAtLimit:
            return String(localized: "devicelimit_still_at_limit", defaultValue: "Still at device limit. Please remove another device.")
        }
    }
}

enum DeviceLimitRetry: Equatable {
    case signedIn, stillAtLimit, failed
}

enum DeviceLimitText {
    static var noneLeft: String {
        String(localized: "devicelimit_none_left", defaultValue: "No devices found. You can continue signing in now.")
    }

    static var unnamed: String {
        String(localized: "devicelimit_unnamed", defaultValue: "Unnamed device")
    }

    static func name(_ device: Device) -> String {
        let trimmed = device.friendlyName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmed.isEmpty ? unnamed : trimmed
    }

    static func added(_ device: Device, locale: Locale = .current, timeZone: TimeZone = .current) -> String? {
        guard let date = HubDate.medium(device.createdAt, locale: locale, timeZone: timeZone) else { return nil }
        return String(format: String(localized: "devicelimit_added", defaultValue: "Added %@"), date)
    }

    // The session store reports a repeat device limit only by staying on this screen with no error.
    static func retryOutcome(signedIn: Bool, showingLimit: Bool, loginError: String?) -> DeviceLimitRetry {
        if signedIn { return .signedIn }
        if showingLimit && loginError == nil { return .stillAtLimit }
        return .failed
    }
}

private struct DeviceLimitRow: View {
    let device: Device
    let removing: Bool
    let enabled: Bool
    let onRemove: () -> Void

    private var removeLabel: String {
        removing
            ? String(localized: "devicelimit_removing", defaultValue: "Removing...")
            : String(localized: "devices_remove", defaultValue: "Remove")
    }

    var body: some View {
        HStack(spacing: 10) {
            info
            Button(action: onRemove) {
                Text(removeLabel)
                    .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                    .foregroundStyle(Palette.error.opacity(enabled || removing ? 1 : 0.45))
                    .padding(.horizontal, 12)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(!enabled)
            // Every row has a Remove button, so VoiceOver also hears which device it removes.
            .accessibilityLabel([removeLabel, DeviceLimitText.name(device)].joined(separator: ", "))
        }
        .padding(.leading, 14)
        .padding(.trailing, 4)
        .padding(.vertical, 6)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).stroke(Palette.borderSubtle, lineWidth: 1))
    }

    private var info: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Text(DeviceLimitText.name(device))
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                    .foregroundStyle(Palette.text)
                    .lineLimit(1)
                    .truncationMode(.tail)
                if device.current {
                    Text(String(localized: "devicelimit_this_device", defaultValue: "This device"))
                        .scaledFont(size: 11, weight: .semibold, relativeTo: .caption2)
                        .foregroundStyle(Palette.accent)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 2)
                        .background(Palette.accent.opacity(0.14), in: Capsule())
                        .lineLimit(1)
                }
            }
            if let added = DeviceLimitText.added(device) {
                Text(added)
                    .scaledFont(size: 12, relativeTo: .caption)
                    .foregroundStyle(Palette.textMuted)
                    .lineLimit(1)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

#if DEBUG
@MainActor
private func previewStore(devices: [Device], loginError: String? = nil) -> SessionStore {
    let core = FakeCore()
    core.devicesResult = .success(devices)
    let store = SessionStore(core: core, tunnel: FakeTunnel())
    store.showDeviceLimit = true
    store.loginError = loginError
    return store
}

private let previewDevices = [
    Device(id: "9f2c1a7e-44b0", friendlyName: "Work laptop", createdAt: "2026-09-01T10:00:00Z", status: "active"),
    Device(id: "b81d03c5-7a21", friendlyName: "Pixel 8", createdAt: "2026-09-12T08:30:00Z", status: "active"),
    Device(id: "c0ffee00-1234", friendlyName: nil, createdAt: "2026-10-02T19:45:00Z", status: "active", current: true)
]

#Preview("Devices") {
    DeviceLimitView()
        .environment(previewStore(devices: previewDevices))
        .environment(ToastCenter())
}

#Preview("Empty") {
    DeviceLimitView()
        .environment(previewStore(devices: []))
        .environment(ToastCenter())
}

#Preview("Retry error, dark") {
    DeviceLimitView()
        .environment(previewStore(devices: previewDevices, loginError: "hub unreachable: every connection method failed"))
        .environment(ToastCenter())
        .preferredColorScheme(.dark)
}
#endif
