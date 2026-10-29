import SwiftUI

struct StoreSwitchRow: View {
    let title: String
    let value: Bool
    var hint: String?
    let apply: (Bool) async -> Void

    @State private var pending: Bool?

    var body: some View {
        SwitchRow(
            title,
            isOn: Binding(
                get: { pending ?? value },
                set: { newValue in
                    pending = newValue
                    Task {
                        await apply(newValue)
                        pending = nil
                    }
                }
            ),
            hint: hint
        )
    }
}

enum SettingsInput {
    static func asciiDigits(_ text: String) -> String {
        text.compactMap { $0.wholeNumberValue }
            .filter { (0...9).contains($0) }
            .map(String.init)
            .joined()
    }
}
