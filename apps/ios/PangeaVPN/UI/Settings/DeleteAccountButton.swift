import StoreKit
import SwiftUI

// Deleting never touches an App Store subscription, so the confirmation says so and offers Apple's own sheet.
struct DeleteAccountButton<Row: View>: View {
    @Environment(SessionStore.self) private var session
    @Environment(PurchaseStore.self) private var purchases
    @Environment(ToastCenter.self) private var toasts
    @State private var confirming = false
    @State private var managing = false
    private let row: Row
    private let onSignedOut: () -> Void

    init(row: Row, onSignedOut: @escaping () -> Void) {
        self.row = row
        self.onSignedOut = onSignedOut
    }

    var body: some View {
        Button(action: ask) {
            row
                .opacity(session.deletingAccount ? 0.5 : 1)
                .overlay(alignment: .trailing) {
                    if session.deletingAccount {
                        ProgressView()
                            .tint(Palette.error)
                            .padding(.trailing, 14)
                    }
                }
        }
        .buttonStyle(.plain)
        .disabled(session.deletingAccount || session.signingOut)
        .confirmationDialog(
            String(localized: "settings_delete_title", defaultValue: "Delete your account?"),
            isPresented: $confirming,
            titleVisibility: .visible
        ) {
            Button(String(localized: "settings_delete_account", defaultValue: "Delete Account"), role: .destructive, action: delete)
            if purchases.appleBilled {
                Button(String(localized: "subscription_manage", defaultValue: "Manage Subscription")) { managing = true }
            }
            Button(String(localized: "settings_home_cancel", defaultValue: "Cancel"), role: .cancel) {}
        } message: {
            Text(String(localized: "settings_delete_body", defaultValue: "This permanently deletes your PangeaVPN account, its remaining time and every device on it. It can't be undone. Deleting doesn't cancel an Apple subscription: cancel that separately in your Apple ID settings."))
        }
        .manageSubscriptionsSheet(isPresented: $managing)
    }

    private func ask() {
        confirming = true
        Task { await purchases.refreshEntitlements() }
    }

    // Deletion clears the session, which also swaps the screen under settings to sign-in.
    private func delete() {
        Task {
            let deleted = await session.deleteAccount()
            if deleted {
                toasts.show(String(localized: "settings_delete_done", defaultValue: "Your account was deleted."), style: .success)
            } else {
                toasts.show(String(localized: "settings_delete_failed", defaultValue: "Couldn't delete your account. Check your connection and try again."), style: .warning)
            }
            if session.session == nil { onSignedOut() }
        }
    }
}
