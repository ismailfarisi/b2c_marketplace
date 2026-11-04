import Foundation
#if canImport(Sentry)
import Sentry
#endif

/// Crash reporting, on when the Sentry package is linked and `SentryDSN` is set
/// in Info.plist. Without either it does nothing, so the app builds without it.
enum Crash {
    static func start() {
        #if canImport(Sentry)
        guard let dsn = Bundle.main.object(forInfoDictionaryKey: "SentryDSN") as? String,
              !dsn.isEmpty else { return }
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        SentrySDK.start { options in
            options.dsn = dsn
            options.releaseName = "ae.marketplace.mobile@\(version)"
            #if DEBUG
            options.environment = "development"
            #else
            options.environment = "production"
            #endif
            options.sendDefaultPii = false
        }
        #endif
    }

    /// Reports a handled error (e.g. APNs registration failing) when crash
    /// reporting is on; logs it otherwise.
    static func report(_ error: Error) {
        #if canImport(Sentry)
        SentrySDK.capture(error: error)
        #else
        print("reported error: \(error)")
        #endif
    }
}
