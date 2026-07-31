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
            RootView(env: env)
        }
    }
}

/// The tab host.
///
/// Deliberately not a `TabView`. TabView owns its own bar, and the bar it draws
/// is opaque and sits *below* the content — which would letterbox the reel above
/// it and give back the full-bleed frame the product is built on. The bar here
/// floats over the video, so it is an overlay and the switching is a plain enum.
struct RootView: View {
    @ObservedObject var env: AppEnvironment

    /// @SceneStorage, not @State: a backgrounded app relaunched by the system
    /// must not drop the viewer back to the feed from wherever they were.
    @SceneStorage("destination") private var destinationRaw: String = Destination.feed.rawValue

    /// One view model for the process. Created once and held, because the feed
    /// stays alive across tab changes — see the comment on the ZStack below.
    @State private var feedViewModel: FeedViewModel?

    private var destination: Destination {
        Destination(rawValue: destinationRaw) ?? .feed
    }

    var body: some View {
        ZStack(alignment: .bottom) {
            Color.black.ignoresSafeArea()

            if let vm = feedViewModel {
                // The feed stays composed across tab changes rather than being
                // rebuilt. Recreating it would drop the scroll position, the
                // loaded pages and the decoder pool, so returning from Saved
                // would cost a fresh network page and the viewer's place — and
                // the feed is the one screen where losing your place is the
                // whole cost.
                FeedScreen(viewModel: vm)
                    .opacity(destination == .feed ? 1 : 0)

                if destination != .feed {
                    NotBuiltYet(destination: destination)
                }
            } else {
                ProgressView().tint(.white)
            }

            BottomNav(selected: destination) { destinationRaw = $0.rawValue }
        }
        .task {
            await env.start()
            if feedViewModel == nil, let sdk = env.sdk {
                feedViewModel = sdk.createFeedViewModel()
            }
        }
        .onChange(of: destination) { _, new in
            // Video stops when the viewer leaves the feed. Without this the reel
            // keeps decoding — and keeps its audio — under a screen that is not
            // showing it: a battery cost, and watch time credited to a reel
            // nobody is watching.
            guard let vm = feedViewModel else { return }
            if new == .feed { vm.onAppForegrounded() } else { vm.onAppBackgrounded() }
        }
    }
}
