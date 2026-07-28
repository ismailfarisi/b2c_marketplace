import SwiftUI
import Shared

/// One SDK per launch, built off the main thread.
///
/// `MarketplaceSdk.create` is suspending on the Kotlin side, which Swift sees as
/// an `async` function — the token store and the device id both touch disk, and
/// doing that during launch on the main thread is a visible delay before the
/// first frame of a video feed.
@MainActor
final class AppEnvironment: ObservableObject {
    @Published private(set) var sdk: MarketplaceSdk?

    /// One session id per process launch.
    ///
    /// The server dedupes views per (user, reel, session). A session that
    /// changed per screen would let one viewer count several times; one that
    /// persisted across launches would stop a genuine return visit counting at
    /// all.
    private let sessionId = UUID().uuidString

    func start() async {
        guard sdk == nil else { return }
        do {
            sdk = try await MarketplaceSdk.companion.create(
                context: PlatformContext(),
                baseUrl: Config.apiBaseUrl,
                locale: Locale.current.language.languageCode?.identifier ?? "en",
                sessionId: sessionId
            )
        } catch {
            // Nothing to retry against yet — the failure is a disk read, and a
            // second attempt in the same second will fail the same way.
            print("sdk init failed: \(error)")
        }
    }
}

enum Config {
    /// The simulator reaches the host directly, unlike Android's 10.0.2.2.
    static let apiBaseUrl = "http://localhost:8080/v1"
}

@main
struct MarketplaceApp: App {
    @StateObject private var env = AppEnvironment()

    var body: some Scene {
        WindowGroup {
            ZStack {
                Color.black.ignoresSafeArea()
                if let sdk = env.sdk {
                    FeedScreen(viewModel: sdk.createFeedViewModel())
                } else {
                    ProgressView().tint(.white)
                }
            }
            .task { await env.start() }
        }
    }
}
