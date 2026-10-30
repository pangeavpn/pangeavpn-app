import SwiftUI
import UIKit

extension SettingsSummary {
    static func developer(_ settings: SettingsStore) -> String {
        settings.developerMode
            ? String(localized: "developer_summary_on", defaultValue: "On")
            : off()
    }
}

struct DeveloperPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(DiagnosticsStore.self) private var diagnostics
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.dismiss) private var dismiss
    @State private var loadingApp = false
    @State private var loadingTunnel = false

    init() {}

    var body: some View {
        SettingsPage(.developer, description: String(localized: "developer_description", defaultValue: "Debugging diagnostics. Leave these off unless you were asked to turn one on.")) {
            LogPanel(
                title: String(localized: "developer_app_log", defaultValue: "App log"),
                text: diagnostics.appLog,
                emptyText: String(localized: "developer_log_empty", defaultValue: "Nothing logged yet."),
                loading: loadingApp,
                onRefresh: { Task { await refreshApp() } },
                onCopy: { copyLog(diagnostics.appLog) })
            LogPanel(
                title: String(localized: "developer_tunnel_log", defaultValue: "Tunnel log"),
                text: diagnostics.extensionLog,
                emptyText: diagnostics.extensionLog == nil
                    ? String(localized: "developer_tunnel_log_unavailable", defaultValue: "The tunnel log is available while the VPN is on.")
                    : String(localized: "developer_log_empty", defaultValue: "Nothing logged yet."),
                loading: loadingTunnel,
                onRefresh: { Task { await refreshTunnel() } },
                onCopy: { copyLog(diagnostics.extensionLog ?? "") })
            SettingsCard {
                GhostButton(String(localized: "developer_copy_diagnostics", defaultValue: "Copy Diagnostics")) {
                    Task { await copyDiagnostics() }
                }
                InfoNote(String(localized: "developer_copy_diagnostics_hint", defaultValue: "Copies the app version, connection status, settings and recent logs. Your account number is never included."))
            }
            SettingsCard {
                GhostButton(String(localized: "developer_turn_off", defaultValue: "Turn Off Developer Options")) {
                    settings.setDeveloperMode(false)
                    dismiss()
                }
            }
        }
        .task {
            await refreshApp()
            await refreshTunnel()
        }
    }

    private func refreshApp() async {
        loadingApp = true
        defer { loadingApp = false }
        _ = await diagnostics.appLogs()
    }

    private func refreshTunnel() async {
        loadingTunnel = true
        defer { loadingTunnel = false }
        _ = await diagnostics.extensionLogs()
    }

    private func copyLog(_ text: String) {
        guard !text.isEmpty else {
            toasts.show(String(localized: "developer_no_logs", defaultValue: "No logs to copy."), style: .warning)
            return
        }
        UIPasteboard.general.string = text
        toasts.show(String(localized: "support_copied", defaultValue: "Copied"), style: .success)
    }

    // Reads both logs and the stored settings first, so the summary is current, not defaults.
    private func copyDiagnostics() async {
        await refreshApp()
        await refreshTunnel()
        if !settings.loaded { await settings.load() }
        UIPasteboard.general.string = diagnostics.diagnosticsJSON(
            status: diagnostics.currentStatus, settings: settings.settings, profile: settings.tunnelProfile)
        toasts.show(String(localized: "support_copied", defaultValue: "Copied"), style: .success)
    }
}

// A fixed-height, selectable, monospaced log that opens scrolled to the newest line.
private struct LogPanel: View {
    let title: String
    let text: String?
    let emptyText: String
    let loading: Bool
    let onRefresh: () -> Void
    let onCopy: () -> Void

    private var shown: String? {
        guard let text, !text.isEmpty else { return nil }
        return text
    }

    var body: some View {
        SettingsCard {
            HStack(spacing: 4) {
                SectionLabel(title)
                    .padding(.leading, 4)
                Spacer(minLength: 8)
                if loading {
                    ProgressView()
                        .controlSize(.small)
                        .frame(width: 44, height: 44)
                } else {
                    iconButton("arrow.clockwise",
                               label: String(localized: "developer_refresh", defaultValue: "Refresh"),
                               action: onRefresh)
                }
                iconButton("doc.on.doc",
                           label: String(localized: "developer_copy_logs", defaultValue: "Copy Logs"),
                           action: onCopy)
                    .disabled(shown == nil)
                    .opacity(shown == nil ? 0.4 : 1)
            }
            ScrollView {
                Text(shown ?? emptyText)
                    .scaledFont(size: 11, design: .monospaced, relativeTo: .caption2)
                    .foregroundStyle(shown == nil ? Palette.textMuted : Palette.textSecondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(12)
            }
            .defaultScrollAnchor(.bottom)
            .frame(height: 240)
            .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
        }
    }

    private func iconButton(_ systemImage: String, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .scaledFont(size: 15, weight: .semibold, relativeTo: .body)
                .foregroundStyle(Palette.textSecondary)
                .frame(width: 44, height: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

#if DEBUG
#Preview("Developer") {
    let core = FakeCore()
    core.logsValue = "2026-10-29T12:00:01.000Z [info] [tunnel] started\n2026-10-29T12:00:02.000Z [info] [hub] reached by reality"
    let tunnel = FakeTunnel()
    tunnel.logsValue = "2026-10-29T12:00:01.500Z [info] [extension] tunnel up"
    return NavigationStack {
        DeveloperPage()
    }
    .toastOverlay()
    .environment(SettingsStore(core: core))
    .environment(DiagnosticsStore(core: core, tunnel: tunnel))
    .environment(ToastCenter())
}
#endif
