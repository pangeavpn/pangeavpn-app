import SwiftUI

struct StoreSwitchRow: View {
    let title: String
    let value: Bool
    var hint: String?
    let apply: (Bool) async -> Void

    @State private var pending: PendingSwitch?

    var body: some View {
        SwitchRow(
            title,
            isOn: Binding(
                get: { pending?.value ?? value },
                set: { newValue in
                    let tap = PendingSwitch(value: newValue, id: UUID())
                    pending = tap
                    Task {
                        await apply(newValue)
                        // Only the newest tap clears, so an older save cannot flip the switch back mid-flight.
                        if pending?.id == tap.id { pending = nil }
                    }
                }
            ),
            hint: hint
        )
    }
}

private struct PendingSwitch: Equatable {
    let value: Bool
    let id: UUID
}

enum SettingsInput {
    static func asciiDigits(_ text: String) -> String {
        text.compactMap { $0.wholeNumberValue }
            .filter { (0...9).contains($0) }
            .map(String.init)
            .joined()
    }
}
