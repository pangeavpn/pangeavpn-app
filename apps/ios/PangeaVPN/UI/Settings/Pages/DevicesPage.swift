import SwiftUI

struct DevicesPage: View {
    @Environment(SessionStore.self) private var session
    @Environment(ToastCenter.self) private var toasts
    @State private var attempted = false
    @State private var renaming: Device?
    @State private var draftName = ""
    @State private var removing: Device?
    @State private var busyId: String?

    init() {}

    var body: some View {
        SettingsPage(.devices, description: String(localized: "devices_subtitle", defaultValue: "Devices signed in to your account.")) {
            if let message = session.deviceError {
                InfoNote(message, systemImage: "exclamationmark.circle", tint: Palette.error)
            }
            list
        }
        .refreshable { await session.loadDevices() }
        .task { await load() }
        .alert(String(localized: "devices_rename", defaultValue: "Rename"), isPresented: renameShown, presenting: renaming) { device in
            TextField(String(localized: "devices_rename_label", defaultValue: "Device name"), text: nameField)
                .textInputAutocapitalization(.words)
            Button(String(localized: "devices_rename_save", defaultValue: "Save")) { save(device) }
                .disabled(DeviceRename.validated(draftName) == nil)
            Button(String(localized: "devices_rename_cancel", defaultValue: "Cancel"), role: .cancel) {}
        }
        .confirmationDialog(removeTitle, isPresented: removeShown, titleVisibility: .visible, presenting: removing) { device in
            Button(String(localized: "devices_remove", defaultValue: "Remove"), role: .destructive) { remove(device) }
            Button(String(localized: "devices_rename_cancel", defaultValue: "Cancel"), role: .cancel) {}
        } message: { _ in
            Text(String(localized: "devices_remove_body", defaultValue: "That device is signed out and can't connect until it signs in again."))
        }
    }

    @ViewBuilder
    private var list: some View {
        let devices = DeviceNames.currentFirst(session.devices)
        if devices.isEmpty && (session.loadingDevices || !attempted) {
            ProgressView()
                .tint(Palette.accent)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 40)
        } else if devices.isEmpty {
            if session.deviceError == nil {
                Text(String(localized: "devices_none", defaultValue: "No devices found."))
                    .scaledFont(size: 15, relativeTo: .body)
                    .foregroundStyle(Palette.textMuted)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 40)
            }
        } else {
            VStack(spacing: 10) {
                ForEach(devices, id: \.id) { device in
                    DeviceCard(
                        device: device,
                        busy: busyId == device.id,
                        onRename: { startRename(device) },
                        onRemove: { removing = device }
                    )
                    .disabled(busyId != nil)
                }
            }
        }
    }

    private var renameShown: Binding<Bool> {
        Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })
    }

    private var removeShown: Binding<Bool> {
        Binding(get: { removing != nil }, set: { if !$0 { removing = nil } })
    }

    private var nameField: Binding<String> {
        Binding(get: { draftName }, set: { draftName = DeviceNames.clamped($0) })
    }

    private var removeTitle: String {
        let name = removing.map(DeviceNames.display) ?? ""
        return String(format: String(localized: "devices_remove_title", defaultValue: "Remove %@?"), name)
    }

    private func load() async {
        await session.loadDevices()
        attempted = true
    }

    private func startRename(_ device: Device) {
        draftName = DeviceNames.clamped(device.friendlyName ?? "")
        renaming = device
    }

    // A refusal also stays in session.deviceError, which the page shows above the list.
    private func save(_ device: Device) {
        guard busyId == nil else { return }
        guard let name = DeviceRename.validated(draftName) else {
            toasts.show(DeviceRename.lengthMessage, style: .warning)
            return
        }
        busyId = device.id
        Task {
            let renamed = await session.renameDevice(device.id, to: name)
            busyId = nil
            if renamed {
                toasts.show(String(localized: "devices_renamed", defaultValue: "Device renamed."), style: .success)
            } else {
                showFailure()
            }
        }
    }

    // The current device leaves only by signing out, which also frees its slot.
    private func remove(_ device: Device) {
        guard busyId == nil, !device.current else { return }
        busyId = device.id
        Task {
            await session.removeDevice(device.id)
            busyId = nil
            if session.deviceError == nil {
                toasts.show(String(localized: "devices_removed", defaultValue: "Device removed."), style: .success)
            } else {
                showFailure()
            }
        }
    }

    private func showFailure() {
        let fallback = String(localized: "network_update_failed", defaultValue: "Failed to update setting.")
        toasts.show(session.deviceError ?? fallback, style: .warning)
    }
}

// How the list names and orders devices; the naming rule itself is DeviceRename's.
enum DeviceNames {
    // Whole characters only, so an emoji is never cut in half at the hub's limit.
    static func clamped(_ raw: String) -> String {
        var units = 0
        let kept = raw.prefix { ch in
            units += ch.utf16.count
            return units <= DeviceRename.maxUnits
        }
        return String(kept)
    }

    static func display(_ device: Device) -> String {
        let name = device.friendlyName?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return name.isEmpty ? String(localized: "devices_unnamed", defaultValue: "Unnamed device") : name
    }

    static func currentFirst(_ devices: [Device]) -> [Device] {
        devices.filter(\.current) + devices.filter { !$0.current }
    }
}

private struct DeviceCard: View {
    let device: Device
    let busy: Bool
    let onRename: () -> Void
    let onRemove: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            details
            HStack(spacing: 8) {
                DeviceActionButton(String(localized: "devices_rename", defaultValue: "Rename"), filled: true, action: onRename)
                DeviceActionButton(String(localized: "devices_remove", defaultValue: "Remove"), filled: false, action: onRemove)
                    .disabled(device.current)
                Spacer(minLength: 0)
                if busy {
                    ProgressView()
                        .tint(Palette.textSecondary)
                }
            }
            if device.current {
                InfoNote(String(localized: "devices_current_note", defaultValue: "Sign out to remove this device"))
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Palette.surface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
    }

    private var details: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(DeviceNames.display(device))
                    .scaledFont(size: 16, weight: .semibold, relativeTo: .body)
                    .foregroundStyle(Palette.text)
                    .lineLimit(2)
                if device.current {
                    Text(String(localized: "devices_this_device", defaultValue: "This device"))
                        .scaledFont(size: 11, weight: .semibold, relativeTo: .caption)
                        .foregroundStyle(Palette.accent)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 3)
                        .background(Palette.accent.opacity(0.14), in: Capsule())
                        .fixedSize()
                }
            }
            if let added = HubDate.medium(device.createdAt) {
                Text(String(format: String(localized: "devices_added", defaultValue: "Added %@"), added))
                    .scaledFont(size: 13, relativeTo: .footnote)
                    .foregroundStyle(Palette.textMuted)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

private struct DeviceActionButton: View {
    @Environment(\.isEnabled) private var isEnabled
    let title: String
    let filled: Bool
    let action: () -> Void

    init(_ title: String, filled: Bool, action: @escaping () -> Void) {
        self.title = title
        self.filled = filled
        self.action = action
    }

    var body: some View {
        Button(action: action) {
            Text(title)
                .scaledFont(size: 14, weight: .semibold, relativeTo: .subheadline)
                .foregroundStyle(filled ? Palette.onAccent : Palette.error)
                .padding(.horizontal, 18)
                .frame(minHeight: 44)
                .background(filled ? Palette.accent : Color.clear, in: Capsule())
                .overlay(Capsule().strokeBorder(filled ? Color.clear : Palette.error.opacity(0.55), lineWidth: 1))
                .contentShape(Capsule())
                .opacity(isEnabled ? 1 : 0.4)
        }
        .buttonStyle(PressFadeStyle())
    }
}

#if DEBUG
@MainActor
private func previewDevices(_ result: Result<[Device], CoreError>) -> some View {
    let core = FakeCore()
    core.devicesResult = result
    return NavigationStack { DevicesPage() }
        .toastOverlay()
        .environment(SessionStore(core: core, tunnel: FakeTunnel()))
        .environment(SettingsStore(core: core))
        .environment(ToastCenter())
}

private let previewList = [
    Device(id: "9f2c1a7e-44b0", friendlyName: "Work laptop", createdAt: "2026-09-01T10:00:00Z", status: "active"),
    Device(id: "c0ffee00-1234", friendlyName: "Swift Fox", createdAt: "2026-06-14T19:45:00Z", status: "active", current: true),
    Device(id: "b81d03c5-7a21", friendlyName: nil, createdAt: "2026-08-02T08:30:00Z", status: "active"),
]

#Preview("Devices") {
    previewDevices(.success(previewList))
}

#Preview("Empty, dark") {
    previewDevices(.success([]))
        .preferredColorScheme(.dark)
}

#Preview("Load failed") {
    previewDevices(.failure(CoreError(message: "Could not reach the account server.")))
}
#endif
