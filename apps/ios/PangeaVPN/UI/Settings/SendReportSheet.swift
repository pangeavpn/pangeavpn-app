import SwiftUI
import UIKit

enum ReportPhase: Equatable {
    case compose, sending
    case sent(code: String)
    case failed(ReportFailure)
}

// Desktop's Send Diagnostics dialog: explain, take an optional note, then show the code or why not.
struct SendReportSheet: View {
    @Environment(DiagnosticsStore.self) private var diagnostics
    @Environment(\.dismiss) private var dismiss
    @State private var note = ""
    @State private var phase: ReportPhase
    @State private var copied = false

    private static let copiedFor = Duration.milliseconds(1600)

    init(phase: ReportPhase = .compose) {
        _phase = State(initialValue: phase)
    }

    private var sending: Bool { phase == .sending }

    var body: some View {
        NavigationStack {
            ScrollView {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.top, 8)
                    .padding(.bottom, 24)
            }
            .scrollDismissesKeyboard(.interactively)
            .background(Palette.bg.ignoresSafeArea())
            .navigationTitle(String(localized: "support_send_report", defaultValue: "Send Report"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackground(Palette.bg, for: .navigationBar)
        }
        .privacyCover()
        .interactiveDismissDisabled(sending)
        .presentationDragIndicator(sending ? .hidden : .visible)
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: Self.copiedFor)
            guard !Task.isCancelled else { return }
            copied = false
        }
    }

    @ViewBuilder
    private var content: some View {
        switch phase {
        case .compose, .sending:
            compose
        case .sent(let code):
            sent(code)
        case .failed(let failure):
            failed(failure)
        }
    }

    private var compose: some View {
        VStack(alignment: .leading, spacing: 16) {
            paragraph(String(localized: "support_report_body", defaultValue: "This sends your app log and the VPN service log to support. Account numbers and keys are stripped out first, and so is your IP address."))
            paragraph(String(localized: "support_report_anonymous", defaultValue: "The report is anonymous. We cannot tell who sent it, so you will get a short code to quote in your email."))
            noteField
            VStack(spacing: 10) {
                PillButton(
                    sending
                        ? String(localized: "support_sending", defaultValue: "Sending…")
                        : String(localized: "support_send", defaultValue: "Send"),
                    enabled: !sending,
                    action: send)
                GhostButton(String(localized: "support_cancel", defaultValue: "Cancel")) { dismiss() }
                    .disabled(sending)
                    .opacity(sending ? 0.5 : 1)
            }
            .padding(.top, 4)
        }
    }

    private var noteField: some View {
        let label = String(localized: "support_note_label", defaultValue: "Note (optional)")
        let limit = DiagnosticsReport.noteLimit
        return VStack(alignment: .leading, spacing: 6) {
            Text(label)
                .scaledFont(size: 13, weight: .semibold, relativeTo: .footnote)
                .foregroundStyle(Palette.textSecondary)
                .accessibilityHidden(true)
            TextField(
                label, text: $note,
                prompt: Text(String(localized: "support_note_placeholder", defaultValue: "What went wrong?"))
                    .foregroundStyle(Palette.textMuted),
                axis: .vertical)
                .lineLimit(3...6)
                .scaledFont(size: 15, relativeTo: .subheadline)
                .foregroundStyle(Palette.text)
                .tint(Palette.accent)
                .textInputAutocapitalization(.sentences)
                .disabled(sending)
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
                .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
                .onChange(of: note) { _, value in
                    if value.count > limit { note = String(value.prefix(limit)) }
                }
            Text(verbatim: "\(note.count)/\(limit)")
                .scaledFont(size: 12, design: .monospaced, relativeTo: .caption)
                .foregroundStyle(Palette.textMuted)
                .frame(maxWidth: .infinity, alignment: .trailing)
                .accessibilityHidden(true)
        }
    }

    private func sent(_ code: String) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            heading(String(localized: "support_sent_title", defaultValue: "Report sent"),
                    systemImage: "checkmark.circle.fill", tint: Palette.connected)
            Text(code)
                .scaledFont(size: 28, weight: .semibold, design: .monospaced, relativeTo: .title)
                .foregroundStyle(Palette.text)
                .textSelection(.enabled)
                .minimumScaleFactor(0.6)
                .lineLimit(1)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 18)
                .padding(.horizontal, 12)
                .background(Palette.surface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.borderSubtle, lineWidth: 1))
            paragraph(String(localized: "support_sent_body", defaultValue: "Quote this code in your email to support."))
            VStack(spacing: 10) {
                PillButton(
                    copied
                        ? String(localized: "support_copied", defaultValue: "Copied")
                        : String(localized: "support_copy_code", defaultValue: "Copy Code"),
                    enabled: !copied
                ) { copy(code) }
                GhostButton(String(localized: "settings_close", defaultValue: "Close")) { dismiss() }
            }
            .padding(.top, 4)
        }
    }

    private func failed(_ failure: ReportFailure) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            heading(String(localized: "support_failed_title", defaultValue: "Could not send"),
                    systemImage: "exclamationmark.triangle.fill", tint: Palette.error)
            paragraph(failure == .rejected
                ? String(localized: "support_rejected_body", defaultValue: "The server refused the report. Wait a minute and try again.")
                : String(localized: "support_failed_body", defaultValue: "The report did not reach us. Check your internet connection and try again."))
            VStack(spacing: 10) {
                PillButton(String(localized: "support_try_again", defaultValue: "Try Again")) { phase = .compose }
                GhostButton(String(localized: "settings_close", defaultValue: "Close")) { dismiss() }
            }
            .padding(.top, 4)
        }
    }

    private func heading(_ text: String, systemImage: String, tint: Color) -> some View {
        HStack(spacing: 10) {
            Image(systemName: systemImage)
                .scaledFont(size: 22, weight: .semibold, relativeTo: .title2)
                .foregroundStyle(tint)
                .accessibilityHidden(true)
            Text(text)
                .scaledFont(size: 20, weight: .bold, relativeTo: .title3)
                .foregroundStyle(Palette.text)
                .accessibilityAddTraits(.isHeader)
        }
    }

    private func paragraph(_ text: String) -> some View {
        Text(text)
            .scaledFont(size: 14, relativeTo: .subheadline)
            .foregroundStyle(Palette.textSecondary)
            .fixedSize(horizontal: false, vertical: true)
    }

    private func send() {
        phase = .sending
        let text = note
        Task {
            switch await diagnostics.send(note: text) {
            case .success(let code):
                phase = .sent(code: code)
            case .failure(let error):
                phase = .failed(ReportFailure(error))
            }
        }
    }

    // The sheet covers the app's toasts, so the button itself confirms the copy, as on desktop.
    private func copy(_ code: String) {
        UIPasteboard.general.string = code
        copied = true
    }
}

#if DEBUG
#Preview("Compose") {
    SendReportSheet()
        .environment(DiagnosticsStore(core: FakeCore(), tunnel: FakeTunnel()))
}

#Preview("Sent") {
    SendReportSheet(phase: .sent(code: "7KQ2-M9XD"))
        .environment(DiagnosticsStore(core: FakeCore(), tunnel: FakeTunnel()))
}

#Preview("Rejected") {
    SendReportSheet(phase: .failed(.rejected))
        .environment(DiagnosticsStore(core: FakeCore(), tunnel: FakeTunnel()))
}
#endif
