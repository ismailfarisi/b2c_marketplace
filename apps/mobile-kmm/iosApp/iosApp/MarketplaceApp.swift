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
    /// The web app, for pages the app links out to (office lunch sign-up).
    static let webBaseUrl = "http://localhost:3000"
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

/// What is drawn *over* the tabs.
///
/// Messaging is not a destination. It is reached from the Me screen and from a
/// CTA press three swipes into the feed, and making it a sixth tab would turn
/// the second into a context switch out of the reel the viewer is watching.
private enum Route: Equatable { case none, inbox, thread }

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
    @SceneStorage("destination") private var destinationRaw: String = Destination.meals.rawValue

    /// One view model for the process. Created once and held, because the feed
    /// stays alive across tab changes — see the ZStack below.
    @State private var feedViewModel: FeedViewModel?

    /// Saved, Me, Search and messaging share one state holder, created once
    /// here rather than per tab: its whole design is that flicking between
    /// those tabs does not re-fetch.
    @State private var meViewModel: MeViewModel?
    @State private var signInViewModel: SignInViewModel?
    @State private var mealsViewModel: MealsViewModel?
    /// A meals action waiting on sign-in: checkout, rotation, My meals.
    @State private var pendingMeals: (() -> Void)?
    @State private var session: Session?
    @State private var sellViewModel: SellViewModel?

    @State private var route: Route = .none
    @State private var focusOpen = false
    @State private var focusTitle = ""

    /// The one answer to "is there an account behind this session".
    ///
    /// Three states, not two. Reading the Keychain is asynchronous, and a
    /// boolean has to pick a side while that read is in flight: `false` flashes
    /// the sign-in gate at a signed-in returning viewer, `true` flashes their
    /// empty Saved list. Neither is fixable with a spinner *inside* the gate,
    /// because the gate itself is the wrong screen.
    @State private var sessionState: SessionState = SessionStateUnknown()

    /// Where the feed is pointed, so search measures distance from it and the
    /// Nearest sort knows whether to appear.
    @State private var lat: Double = 0
    @State private var lng: Double = 0

    private var destination: Destination {
        Destination(rawValue: destinationRaw) ?? .meals
    }

    private var authorised: Bool { sessionState is SessionStateAuthenticated }
    private var settled: Bool { !(sessionState is SessionStateUnknown) }

    /// Three of the five need an account. Search does not — it is the
    /// highest-intent surface in the product and the one most likely to arrive
    /// from a shared link, and a login wall there converts a motivated buyer
    /// into a bounce. The same call the API makes by leaving GET /listings
    /// public.
    private var needsAccount: Bool {
        destination == .sell || destination == .saved || destination == .me || destination == .myMeals
    }

    var body: some View {
        ZStack(alignment: .bottom) {
            Color.black.ignoresSafeArea()

            if let vm = feedViewModel, let me = meViewModel {
                // The feed stays composed across tab changes rather than being
                // rebuilt. Recreating it would drop the scroll position, the
                // loaded pages and the decoder pool, so returning from Saved
                // would cost a fresh network page and the viewer's place — and
                // the feed is the one screen where losing your place is the
                // whole cost.
                FeedScreen(viewModel: vm, extras: [
                    FeedExtra(label: NSLocalizedString("nav_search", comment: ""), symbol: "magnifyingglass") { destinationRaw = Destination.search.rawValue },
                    FeedExtra(label: NSLocalizedString("nav_saved", comment: ""), symbol: "bookmark") { destinationRaw = Destination.saved.rawValue },
                    FeedExtra(label: NSLocalizedString("nav_sell", comment: ""), symbol: "plus.circle") { destinationRaw = Destination.sell.rawValue },
                ])
                    .opacity(destination == .feed ? 1 : 0)

                if destination != .feed {
                    tab(vm: vm, me: me)
                }

                // One reel, over the tabs and under messaging. Under, because
                // its CTA opens a thread and the thread has to be on top of the
                // reel that produced it.
                if focusOpen {
                    ReelDetail(
                        viewModel: vm,
                        returnToIndex: 0,
                        onAction: { action, reelId in
                            handle(action: action, reelId: reelId, me: me, title: focusTitle)
                        },
                        onClose: { focusOpen = false }
                    )
                    .ignoresSafeArea()
                }

                if route != .none {
                    messaging(me: me)
                }
            } else {
                ProgressView().tint(.white)
            }

            // Hidden behind anything modal. A floating bar over a modal is a
            // mis-tap that loses whatever the viewer was doing.
            if destination != .sell && route == .none && !focusOpen {
                BottomNav(selected: destination) { destinationRaw = $0.rawValue }
            }
        }
        .task {
            await env.start()
            guard let sdk = env.sdk else { return }
            if feedViewModel == nil { feedViewModel = sdk.createFeedViewModel() }
            if meViewModel == nil { meViewModel = sdk.createMeViewModel() }
            if signInViewModel == nil { signInViewModel = sdk.createSignInViewModel() }
            if mealsViewModel == nil { mealsViewModel = sdk.createMealsViewModel() }
            if destination != .feed { feedViewModel?.onAppBackgrounded() }
            session = sdk.session
        }
        .task(id: session != nil) {
            guard let session else { return }
            for await state in session.state { sessionState = state }
        }
        .task(id: session != nil) {
            guard let session, let me = meViewModel else { return }
            // A session that ends underneath the viewer takes its screens with
            // it. Without the `forget`, the next person to sign in on this
            // device sees the previous account's inbox — the state holder
            // outlives the session by design, and its `loaded` flags mean it
            // would never re-fetch to correct itself.
            for await ended in session.ended {
                guard let ended else { continue }
                me.forget()
                mealsViewModel?.forget()
                if ended == .expired && destination != .feed && destination != .search {
                    destinationRaw = Destination.feed.rawValue
                }
                session.clearEnded()
            }
        }
        .task(id: feedViewModel != nil) {
            guard let vm = feedViewModel else { return }
            for await state in vm.state {
                lat = state.lat
                lng = state.lng
                meViewModel?.setLocation(lat: state.lat, lng: state.lng)
            }
        }
        .task(id: feedViewModel != nil) {
            guard let vm = feedViewModel else { return }
            // The title travels with the focused reel so a CTA press from it
            // can name the thing in the conversation it opens.
            for await focused in vm.focused {
                focusTitle = focused.item?.summary.map { localised($0.title, $0.titleAr) } ?? ""
            }
        }
        .onChange(of: authorised) { _, ok in
            guard ok, let run = pendingMeals else { return }
            pendingMeals = nil
            run()
        }
        .onChange(of: destination) { _, new in
            // The sell flow's state holder goes when the tab does, taking the
            // recorded clip with it.
            if new != .sell { sellViewModel = nil }

            // Video stops when the viewer leaves the feed. Without this the reel
            // keeps decoding — and keeps its audio — under a screen that is not
            // showing it: a battery cost, and watch time credited to a reel
            // nobody is watching.
            guard let vm = feedViewModel else { return }
            if new == .feed { vm.onAppForegrounded() } else { vm.onAppBackgrounded() }
        }
    }

    // ── the tabs ─────────────────────────────────────────────────────────────

    private var gateCopy: (String, String) {
        switch destination {
        case .myMeals: return ("meals_signin_mine_title", "meals_signin_why")
        case .me: return ("me_signin_title", "me_signin_why")
        case .saved: return ("saved_signin_title", "saved_signin_why")
        default: return ("signin_title", "signin_why")
        }
    }

    @ViewBuilder
    private func tab(vm: FeedViewModel, me: MeViewModel) -> some View {
        if needsAccount && !settled {
            // The token store is still being read. Neither the gate nor the
            // account screen is right yet, and picking one shows the wrong
            // screen to a returning viewer for a frame.
            ProgressView().tint(.white)
        } else if needsAccount && !authorised, let gate = signInViewModel {
            // Saved and Sell open from the feed, so cancelling returns there.
            SignInGate(viewModel: gate, onCancel: {
                gate.reset()
                destinationRaw = (destination == .saved || destination == .sell)
                    ? Destination.feed.rawValue : Destination.meals.rawValue
            }, titleKey: gateCopy.0, whyKey: gateCopy.1, light: destination == .myMeals)
        } else {
            switch destination {
            case .meals, .myMeals:
                if let meals = mealsViewModel {
                    ZStack {
                        MealsScreen(
                            viewModel: meals,
                            lat: lat != 0 || lng != 0 ? lat : 25.0805,
                            lng: lat != 0 || lng != 0 ? lng : 55.1403,
                            requireAccount: { run in
                                if authorised { run() } else { pendingMeals = run }
                            },
                            startOnMine: destination == .myMeals
                        )
                        .id(destination)
                        if pendingMeals != nil && !authorised, let gate = signInViewModel {
                            SignInGate(viewModel: gate, onCancel: { pendingMeals = nil },
                                       titleKey: "meals_signin_title", whyKey: "meals_signin_why")
                                .background(Palette.ink.ignoresSafeArea())
                        }
                    }
                }
            case .search:
                SearchScreen(
                    viewModel: me,
                    hasLocation: lat != 0 || lng != 0,
                    onOpenReel: { reelId in
                        vm.openReel(reelId: reelId)
                        focusOpen = true
                    },
                    onInquire: { result in
                        guard authorised else {
                            destinationRaw = Destination.me.rawValue
                            return
                        }
                        me.inquire(listingId: result.id, title: result.title,
                                   reelId: nil, actionId: nil)
                        route = .thread
                    }
                )
            case .saved:
                SavedScreen(viewModel: me) { reelId in
                    vm.openReel(reelId: reelId)
                    focusOpen = true
                }
            case .me:
                MeScreen(
                    viewModel: me,
                    onOpenInbox: { route = .inbox },
                    onSignOut: {
                        // Clears the token store, which a flag-flip would not:
                        // the credential would stay on the device and a relaunch
                        // would hand the next person a live session. `forget`
                        // happens in the `ended` task, for both exits.
                        Task { try? await session?.signOut() }
                        destinationRaw = Destination.feed.rawValue
                    }
                )
            case .sell:
                // Its state holder is created on entry and released on exit
                // rather than living for the process: it holds the recorded
                // clip in memory, and twenty megabytes kept alive after a
                // seller abandoned the flow is twenty megabytes on the device
                // class least able to spare it.
                SellFlow(
                    viewModel: sellViewModel ?? makeSellViewModel(),
                    onLeave: { destinationRaw = Destination.feed.rawValue },
                    onSeeFeed: {
                        destinationRaw = Destination.feed.rawValue
                        // The seller's reel is new supply at this location and
                        // the feed they are returning to was built before it
                        // existed. Refreshing is the difference between "See it
                        // in the feed" showing it and sending them to look for
                        // nothing.
                        vm.refresh()
                    }
                )
            default:
                EmptyView()
            }
        }
    }

    @ViewBuilder
    private func messaging(me: MeViewModel) -> some View {
        switch route {
        case .inbox:
            InboxScreen(
                viewModel: me,
                onOpen: { c in
                    me.openThread(conversationId: c.id, title: c.listingTitle)
                    route = .thread
                },
                onBack: { route = .none }
            )
        case .thread:
            ThreadScreen(viewModel: me) {
                me.closeThread()
                // Back to the inbox rather than to wherever the thread was
                // opened from. A buyer who messaged from the feed has just
                // acquired an inbox, and this is the moment they learn it
                // exists.
                route = .inbox
            }
        case .none:
            EmptyView()
        }
    }

    /// Built on demand and released when the tab is left — see the note at the
    /// call site. Returns a fresh one rather than nil so the view never has to
    /// render an empty Sell tab while a constructor resolves.
    private func makeSellViewModel() -> SellViewModel {
        let created = env.sdk!.createSellViewModel()
        Task { @MainActor in sellViewModel = created }
        return created
    }

    private func handle(action: ResolvedAction, reelId: String, me: MeViewModel, title: String) {
        // The branch is on `kind`, never on `target_type`. That distinction is
        // the whole action seam: a vertical adding a new *target* costs nothing
        // here, and the set of things a client can physically do is genuinely
        // closed and shipped in the app. Anything unrecognised falls through
        // having recorded the click — a newer server's action must not break an
        // older build.
        switch action.kind {
        case "inquire_listing", "enquire_property":
            guard let targetId = action.targetId else { return }
            guard authorised else {
                destinationRaw = Destination.me.rawValue
                return
            }
            me.inquire(listingId: targetId, title: title, reelId: reelId, actionId: action.id)
            route = .thread
        default:
            break
        }
    }
}
