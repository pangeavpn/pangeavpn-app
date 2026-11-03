import Observation
import UIKit

// The Home Screen quick action that starts a panic wipe; its title stays neutral on purpose.
@MainActor
enum PanicShortcut {
    static let type = "org.pangeavpn.app.panic-wipe"

    static func update(enabled: Bool) {
        UIApplication.shared.shortcutItems = items(UIApplication.shared.shortcutItems ?? [], enabled: enabled)
    }

    static func items(_ current: [UIApplicationShortcutItem], enabled: Bool) -> [UIApplicationShortcutItem] {
        let others = current.filter { !isPanic($0) }
        return enabled ? others + [makeItem()] : others
    }

    static func isPanic(_ item: UIApplicationShortcutItem) -> Bool {
        item.type == type
    }

    static func makeItem() -> UIApplicationShortcutItem {
        UIApplicationShortcutItem(
            type: type,
            localizedTitle: String(localized: "panic_shortcut_title", defaultValue: "Wipe data"),
            localizedSubtitle: nil,
            icon: UIApplicationShortcutIcon(systemImageName: "trash"),
            userInfo: nil
        )
    }
}

// Raised by the quick action or "Wipe now"; RootView runs the wipe and lowers it again.
@MainActor
@Observable
final class PanicTrigger {
    static let shared = PanicTrigger()

    var requested = false

    init() {}

    // The wipe resets the setting that offered the shortcut, so the shortcut goes with it.
    func request() {
        PanicShortcut.update(enabled: false)
        requested = true
    }

    @discardableResult
    func handle(_ item: UIApplicationShortcutItem) -> Bool {
        guard PanicShortcut.isPanic(item) else { return false }
        request()
        return true
    }
}

// Scene-based apps get quick actions on the scene, so every scene gets the panic delegate.
@MainActor
final class PanicAppDelegate: NSObject, UIApplicationDelegate {
    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        // Re-applied each launch, so the title follows the app language and a stale item goes.
        let enabled = SharedPrefs.bool(SharedPrefs.Key.panicWipe, default: false, in: AppGroup.defaults)
        PanicShortcut.update(enabled: enabled)
        return true
    }

    func application(
        _ application: UIApplication,
        configurationForConnecting connectingSceneSession: UISceneSession,
        options: UIScene.ConnectionOptions
    ) -> UISceneConfiguration {
        let configuration = UISceneConfiguration(
            name: connectingSceneSession.configuration.name,
            sessionRole: connectingSceneSession.role
        )
        configuration.delegateClass = PanicSceneDelegate.self
        return configuration
    }
}

@MainActor
final class PanicSceneDelegate: NSObject, UIWindowSceneDelegate {
    // A cold launch from the quick action delivers it here, not through performActionFor.
    func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions) {
        if let item = connectionOptions.shortcutItem {
            PanicTrigger.shared.handle(item)
        }
    }

    func windowScene(
        _ windowScene: UIWindowScene,
        performActionFor shortcutItem: UIApplicationShortcutItem,
        completionHandler: @escaping (Bool) -> Void
    ) {
        completionHandler(PanicTrigger.shared.handle(shortcutItem))
    }
}
