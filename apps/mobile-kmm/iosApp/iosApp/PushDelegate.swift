import SwiftUI
import UserNotifications

/// APNs registration. The token goes to the server through the shared SDK,
/// which holds it until someone is signed in. Requires the Push Notifications
/// capability (aps-environment entitlement) on the app target.
final class PushDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    @MainActor static var onToken: ((String) -> Void)? {
        didSet { if let token = pending, let onToken { pending = nil; onToken(token) } }
    }
    @MainActor private static var pending: String?

    /// Where a tapped notification points (its `link`), for the meals tab.
    @MainActor static var onLink: ((String) -> Void)? {
        didSet { if let link = pendingLink, let onLink { pendingLink = nil; onLink(link) } }
    }
    @MainActor private static var pendingLink: String?

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        Task { @MainActor in
            if let onToken = Self.onToken { onToken(token) } else { Self.pending = token }
        }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        print("push registration failed: \(error)")
    }

    /// Shows banners while the app is open too; a delivery update is still news.
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound, .list])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        if let link = response.notification.request.content.userInfo["link"] as? String {
            Task { @MainActor in
                if let onLink = Self.onLink { onLink(link) } else { Self.pendingLink = link }
            }
        }
        completionHandler()
    }

    /// Asked after sign-in, not at launch.
    static func requestPermission() {
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { granted, _ in
            guard granted else { return }
            DispatchQueue.main.async { UIApplication.shared.registerForRemoteNotifications() }
        }
    }
}
