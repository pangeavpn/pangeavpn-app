import Foundation

enum ProviderMessage: UInt8 {
    case state = 1

    static func reply(to message: Data, core: CoreBridge) throws -> Data? {
        guard let first = message.first, let kind = ProviderMessage(rawValue: first) else { return nil }
        switch kind {
        case .state:
            return try JSONEncoder().encode(core.state())
        }
    }
}
