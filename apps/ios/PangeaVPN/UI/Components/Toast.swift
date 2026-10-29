import Observation
import SwiftUI

enum ToastStyle: Equatable, Sendable {
    case info, success, warning
}

struct Toast: Equatable, Identifiable, Sendable {
    let id: UUID
    let text: String
    let style: ToastStyle
}

// One message at a time; a new one replaces the old so feedback is never stale.
@MainActor
@Observable
final class ToastCenter {
    private(set) var current: Toast?
    @ObservationIgnored private var dismissal: Task<Void, Never>?

    static let visibleFor = Duration.seconds(4)

    init() {}

    func show(_ text: String, style: ToastStyle = .info) {
        let toast = Toast(id: UUID(), text: text, style: style)
        current = toast
        dismissal?.cancel()
        dismissal = Task { [weak self] in
            try? await Task.sleep(for: Self.visibleFor)
            guard !Task.isCancelled else { return }
            self?.dismiss(toast.id)
        }
    }

    func dismiss(_ id: UUID? = nil) {
        guard id == nil || current?.id == id else { return }
        current = nil
    }
}

private struct ToastBanner: View {
    let toast: Toast
    let onTap: () -> Void

    private var tint: Color {
        switch toast.style {
        case .info: return Palette.text
        case .success: return Palette.connected
        case .warning: return Palette.connecting
        }
    }

    private var symbol: String {
        switch toast.style {
        case .info: return "info.circle"
        case .success: return "checkmark.circle"
        case .warning: return "exclamationmark.triangle"
        }
    }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: symbol)
                .foregroundStyle(tint)
                .accessibilityHidden(true)
            Text(toast.text)
                .scaledFont(size: 14, weight: .medium, relativeTo: .subheadline)
                .foregroundStyle(Palette.text)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 12)
        .background(Palette.elevated, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Palette.border, lineWidth: 1))
        .shadow(color: .black.opacity(0.18), radius: 12, y: 4)
        .padding(.horizontal, 16)
        .contentShape(Rectangle())
        .onTapGesture(perform: onTap)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isStaticText)
    }
}

private struct ToastOverlay: ViewModifier {
    @Environment(ToastCenter.self) private var toasts

    func body(content: Content) -> some View {
        content.overlay(alignment: .bottom) {
            if let toast = toasts.current {
                ToastBanner(toast: toast) { toasts.dismiss(toast.id) }
                    .padding(.bottom, 12)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .id(toast.id)
            }
        }
        .animation(.easeOut(duration: 0.2), value: toasts.current)
        .onChange(of: toasts.current) { _, toast in
            guard let toast else { return }
            UIAccessibility.post(notification: .announcement, argument: toast.text)
        }
    }
}

extension View {
    // Needs a ToastCenter in the environment.
    func toastOverlay() -> some View {
        modifier(ToastOverlay())
    }
}

#if DEBUG
#Preview("Toasts") {
    let center = ToastCenter()
    return VStack {
        Button("Info") { center.show("Connection method updated. Reconnect for it to take effect.") }
        Button("Success") { center.show("Device renamed.", style: .success) }
        Button("Warning") { center.show("Could not reach the account server.", style: .warning) }
    }
    .frame(maxWidth: .infinity, maxHeight: .infinity)
    .background(Palette.bg)
    .environment(center)
    .toastOverlay()
}
#endif
