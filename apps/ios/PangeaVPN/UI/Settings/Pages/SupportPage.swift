import SwiftUI
import UIKit

// The address desktop ships as its support contact.
enum SupportContact {
    static let email = "support@pangeavpn.org"
    static let mailURL = URL(string: "mailto:\(email)")
}

struct SupportPage: View {
    @Environment(ToastCenter.self) private var toasts
    @Environment(\.openURL) private var openURL
    @State private var showReport = false
    @State private var showDataNotice = false

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
            SettingsCard {
                Button { showDataNotice = true } label: {
                    SettingsNavRow(
                        systemImage: "hand.raised",
                        title: String(localized: "support_privacy_data", defaultValue: "Privacy and Data"))
                }
                .buttonStyle(.plain)
                legalRow(String(localized: "paywall_privacy_link", defaultValue: "Privacy Policy"),
                         systemImage: "lock.doc", url: PaywallView.privacyURL)
                legalRow(String(localized: "paywall_terms_link", defaultValue: "Terms of Use"),
                         systemImage: "doc.text", url: PaywallView.termsURL)
            }
        }
        .sheet(isPresented: $showReport) {
            SendReportSheet()
        }
        .sheet(isPresented: $showDataNotice) {
            DataNoticeView(mode: .review, onDone: { showDataNotice = false })
                .formSheetSized()
        }
    }

    private func legalRow(_ title: String, systemImage: String, url: URL) -> some View {
        Button { openURL(url) } label: {
            SettingsNavRow(systemImage: systemImage, title: title)
        }
        .buttonStyle(.plain)
    }

    private func contact(_ url: URL) {
        Task {
            let opened = await UIApplication.shared.open(url)
            guard !opened else { return }
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
