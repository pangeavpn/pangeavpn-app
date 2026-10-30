import SwiftUI

// The address desktop ships as its support contact.
enum SupportContact {
    static let email = "support@pangeavpn.org"
    static let mailURL = URL(string: "mailto:\(email)")
}

struct SupportPage: View {
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.openURL) private var openURL
    @State private var showReport = false

    init() {}

    var body: some View {
        SettingsPage(.support, description: String(localized: "support_description", defaultValue: "Send your logs to support with personal details stripped out. You get a code to quote in your email.")) {
            SettingsCard {
                Button { showReport = true } label: {
                    SettingsNavRow(
                        systemImage: "paperplane",
                        title: String(localized: "support_send_report", defaultValue: "Send Report"))
                }
                .buttonStyle(.plain)
                if let url = SupportContact.mailURL {
                    Button { contact(url) } label: {
                        SettingsNavRow(
                            systemImage: "envelope",
                            title: String(localized: "support_contact", defaultValue: "Contact Support"),
                            value: SupportContact.email)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .sheet(isPresented: $showReport) {
            SendReportSheet()
        }
    }

    private func contact(_ url: URL) {
        openURL(url) { accepted in
            guard !accepted else { return }
            let format = String(localized: "support_no_mail_app", defaultValue: "No mail app is set up. Write to %@ instead.")
            toasts.show(String(format: format, SupportContact.email), style: .warning)
        }
    }
}

#if DEBUG
#Preview("Support") {
    NavigationStack {
        SupportPage()
    }
    .environment(SettingsStore(core: FakeCore()))
    .environment(DiagnosticsStore(core: FakeCore(), tunnel: FakeTunnel()))
    .environment(ToastCenter())
}
#endif
