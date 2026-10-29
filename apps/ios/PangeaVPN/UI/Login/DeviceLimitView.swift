import SwiftUI

struct DeviceLimitView: View {
    @Environment(SessionStore.self) private var session
    @State private var busy = false

    init() {}

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
            .padding(.top, 8)

            deviceList
                .padding(.top, 22)

            errorText(session.deviceError)
            errorText(session.loginError)

            PillButton(
                String(localized: "login_signin", defaultValue: "Sign In"),
                loading: session.loggingIn || busy
            ) {
                guard !busy else { return }
                busy = true
                Task {
                    await session.retryLogin()
                    busy = false
                }
            }
            .padding(.top, 6)
            GhostButton(String(localized: "devicelimit_back", defaultValue: "Back")) {
                session.showDeviceLimit = false
                session.loginError = nil
            }
            .padding(.top, 10)
        }
        .padding(.horizontal, 20)
        .padding(.top, 32)
        .padding(.bottom, 24)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Palette.bg.ignoresSafeArea())
        .task { await session.loadDevices() }
    }

    @ViewBuilder
    private func errorText(_ message: String?) -> some View {
        if let message {
            Text(message)
                .scaledFont(size: 13, relativeTo: .footnote)
                .foregroundStyle(Palette.error)
                .padding(.vertical, 10)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func remove(_ id: String) {
        guard !busy else { return }
        busy = true
        Task {
            await session.removeDevice(id)
            busy = false
        }
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
                        DeviceRow(
                            name: device.friendlyName ?? device.id,
                            identifier: device.id
                        ) {
                            remove(device.id)
                        }
                    }
                }
                .padding(.bottom, 12)
            }
        }
    }
}

private struct DeviceRow: View {
    let name: String
    let identifier: String
    let onRemove: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 1) {
                Text(name)
                    .scaledFont(size: 15, weight: .semibold, relativeTo: .subheadline)
                    .foregroundStyle(Palette.text)
                    .lineLimit(1)
                    .truncationMode(.tail)
                if identifier != name {
                    Text(identifier)
                        .scaledFont(size: 11, design: .monospaced, relativeTo: .caption2)
                        .foregroundStyle(Palette.textMuted)
                        .lineLimit(1)
                        .truncationMode(.tail)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: onRemove) {
                Text(String(localized: "devices_remove", defaultValue: "Remove"))
                    .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                    .foregroundStyle(Palette.error)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 8)
            }
            .buttonStyle(.plain)
        }
        .padding(.leading, 14)
        .padding(.trailing, 6)
        .padding(.vertical, 10)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).stroke(Palette.borderSubtle, lineWidth: 1))
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
    Device(id: "c0ffee00-1234", friendlyName: nil, createdAt: "2026-10-02T19:45:00Z", status: "active")
]

#Preview("Devices") {
    DeviceLimitView()
        .environment(previewStore(devices: previewDevices))
}

#Preview("Retry error, dark") {
    DeviceLimitView()
        .environment(previewStore(devices: previewDevices, loginError: "Network unreachable"))
        .preferredColorScheme(.dark)
}
#endif
