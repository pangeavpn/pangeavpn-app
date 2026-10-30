import SwiftUI

struct SplitTunnelPage: View {
    @Environment(SettingsStore.self) private var settings
    @Environment(ToastCenter.self) private var toasts
    @State private var rangeError: String?
    // Bumped on each save, so the field drops what was typed and shows the stored, normalized list.
    @State private var fieldRevision = 0

    init() {}

    var body: some View {
        SettingsPage(.splitTunnel, description: String(localized: "split_tunnel_description", defaultValue: "Addresses listed here bypass the VPN and use your normal connection.")) {
            if settings.killSwitch {
                InfoNote(String(localized: "split_tunnel_note_strict", defaultValue: "Strict kill switch is on, so iOS sends everything through the VPN and these ranges are ignored."), systemImage: "lock.shield", tint: Palette.error)
            }
            SettingsCard {
                StoreSwitchRow(
                    title: String(localized: "split_tunnel_enabled_title", defaultValue: "Use split tunnelling"),
                    value: settings.splitTunnelEnabled,
                    hint: String(localized: "split_tunnel_enabled_hint", defaultValue: "Off sends everything through the VPN. Your ranges are kept. Applies on next connect."),
                    apply: { await setEnabled($0) }
                )
            }
            SettingsCard {
                rangesField
                if let rangeError {
                    InfoNote(rangeError, systemImage: "exclamationmark.circle", tint: Palette.error)
                }
            }
            SettingsCard {
                InfoNote(String(localized: "split_tunnel_note_dns", defaultValue: "Name lookups (DNS) still go through the VPN."))
                InfoNote(String(localized: "split_tunnel_note_lan", defaultValue: "For devices on your own network, turn on Allow LAN instead."), systemImage: "wifi")
                InfoNote(String(localized: "split_tunnel_note_apps", defaultValue: "iPhone can't leave out individual apps: iOS only lets a VPN exclude address ranges."), systemImage: "iphone")
            }
        }
    }

    private var rangesField: some View {
        CommittedField(
            label: String(localized: "split_tunnel_ranges_title", defaultValue: "Excluded IP ranges"),
            hint: String(localized: "split_tunnel_ranges_hint", defaultValue: "IPv4 addresses or ranges, separated by commas. Applies on next connect."),
            placeholder: String(localized: "split_tunnel_ranges_placeholder", defaultValue: "192.0.2.0/24, 198.51.100.7"),
            initial: SplitTunnelRanges.text(settings.splitTunnelRanges),
            keyboard: .numbersAndPunctuation,
            onCommit: { commit($0) }
        )
        .id(fieldRevision)
    }

    private func setEnabled(_ enabled: Bool) async {
        guard enabled != settings.splitTunnelEnabled else { return }
        settings.setSplitTunnelEnabled(enabled)
        if enabled {
            toasts.show(String(localized: "split_tunnel_toast_on", defaultValue: "Split tunnelling on. Applies on next connect."), style: .success)
        } else {
            toasts.show(String(localized: "split_tunnel_toast_off", defaultValue: "Split tunnelling off. Applies on next connect."), style: .info)
        }
    }

    private func commit(_ text: String) {
        switch SplitTunnelRanges.parse(text) {
        case .success(let ranges):
            rangeError = nil
            settings.setSplitTunnelRanges(ranges)
            fieldRevision += 1
            if ranges.isEmpty {
                toasts.show(String(localized: "split_tunnel_toast_cleared", defaultValue: "No IP ranges excluded."), style: .info)
            } else {
                toasts.show(String(localized: "split_tunnel_toast_saved", defaultValue: "Excluded IP ranges saved."), style: .success)
            }
        case .failure(let error):
            rangeError = Self.message(for: error)
        }
    }

    // One sentence per kind of problem, naming the entries it applies to, as desktop words it.
    static func message(for error: SplitTunnelRangesError) -> String {
        let sentences: [String?] = [
            error.notIPv4.isEmpty ? nil : String(format: String(localized: "split_tunnel_error_not_ipv4", defaultValue: "Not an IPv4 address or range: %@."), entries(error.notIPv4)),
            error.prefixTooShort.isEmpty ? nil : String(format: String(localized: "split_tunnel_error_too_broad", defaultValue: "Too broad, use /8 or narrower: %@."), entries(error.prefixTooShort)),
            error.tooMany ? String(localized: "split_tunnel_error_too_many", defaultValue: "Too many ranges. Remove some first.") : nil,
        ]
        return sentences.compactMap { $0 }.joined(separator: " ")
    }

    // Isolated left to right, so an address stays intact inside Arabic or Persian text.
    private static func entries(_ tokens: [String]) -> String {
        tokens.map { "\u{2066}\($0)\u{2069}" }.joined(separator: ", ")
    }
}

extension SettingsSummary {
    static func splitTunnel(_ settings: SettingsStore) -> String {
        guard settings.splitTunnelEnabled else { return off() }
        switch settings.splitTunnelRanges.count {
        case 0:
            return String(localized: "split_tunnel_summary_empty", defaultValue: "Nothing excluded")
        case 1:
            return String(localized: "split_tunnel_summary_one", defaultValue: "1 range")
        case let count:
            return String(format: String(localized: "split_tunnel_summary_count", defaultValue: "%lld ranges"), count)
        }
    }
}

#if DEBUG
#Preview("Split tunnelling") {
    ProtectionPreview.stage(SplitTunnelPage()) {
        $0.setSplitTunnelEnabled(true)
        $0.setSplitTunnelRanges(["192.0.2.0/24", "198.51.100.7/32"])
    }
}

#Preview("Split tunnelling, strict, dark") {
    ProtectionPreview.stage(SplitTunnelPage()) {
        $0.setSplitTunnelEnabled(true)
        $0.setKillSwitch(true)
    }
    .preferredColorScheme(.dark)
}
#endif
