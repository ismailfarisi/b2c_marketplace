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
            if let sdk {
                PushDelegate.onToken = { token in sdk.registerPushToken(token: token, platform: "ios") }
            }
        } catch {
            // Nothing to retry against yet — the failure is a disk read, and a
            // second attempt in the same second will fail the same way.
            print("sdk init failed: \(error)")
        }
    }
}

enum Config {
    /// From Info.plist `ApiBaseUrl` (= $(API_BASE_URL), set per configuration in
    /// Debug.xcconfig / Release.xcconfig). The Debug default is the simulator,
    /// which reaches the host directly, unlike Android's 10.0.2.2.
    static let apiBaseUrl = checked("ApiBaseUrl", value("ApiBaseUrl", fallback: defaultApi))
    /// The web app, for pages the app links out to. Info.plist `WebBaseUrl`.
    static let webBaseUrl = checked("WebBaseUrl", value("WebBaseUrl", fallback: defaultWeb))

    #if DEBUG
    private static let defaultApi = "http://localhost:8080/v1"
    private static let defaultWeb = "http://localhost:3000"
    #else
    private static let defaultApi = "https://api.switeaz.com/v1"
    private static let defaultWeb = "https://switeaz.com"
    #endif

    /// An unset build setting arrives as an empty string or the literal
    /// `$(NAME)`; both mean "not configured".
    private static func value(_ key: String, fallback: String) -> String {
        guard let s = Bundle.main.object(forInfoDictionaryKey: key) as? String,
              !s.isEmpty, !s.hasPrefix("$(") else { return fallback }
        return s
    }

    /// True for a URL a phone cannot reach: no host, or the build machine itself.
    static func isLocal(_ url: String) -> Bool {
        guard let host = URL(string: url)?.host?.lowercased(), !host.isEmpty else { return true }
        return host == "localhost" || host == "127.0.0.1" || host == "0.0.0.0" || host == "::1"
            || host.hasSuffix(".localhost")
    }

    /// A release build pointing at localhost works on the simulator and is dead
    /// on every customer's phone, so it stops at launch instead of shipping.
    private static func checked(_ key: String, _ url: String) -> String {
        #if !DEBUG
        if isLocal(url) {
            fatalError("\(key) is \(url) in a release build. Set API_BASE_URL/WEB_BASE_URL in iosApp/Release.xcconfig.")
        }
        #endif
        return url
    }
}

@main
struct MarketplaceApp: App {
    @UIApplicationDelegateAdaptor(PushDelegate.self) private var pushDelegate
    @StateObject private var env = AppEnvironment()

    init() {
        Crash.start()
    }

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
private enum Route: Equatable { case none, inbox, thread, help, helpTicket, invoices, invoiceDetail }

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
    @State private var supportViewModel: SupportViewModel?
    /// A meals action waiting on sign-in: checkout, rotation, My meals.
    @State private var pendingMeals: (() -> Void)?
    /// Whether that action is Help, so the gate says "get help".
    @State private var pendingMealsHelp = false
    @State private var session: Session?
    @State private var sellViewModel: SellViewModel?
    /// A feed action (like, save, inquiry) waiting on sign-in; replayed once
    /// the session is authorised instead of dropping the viewer on their profile.
    @State private var pendingFeed: (() -> Void)?

    @State private var route: Route = .none
    @State private var link: MealsLink?
    @State private var update: AppUpdate?
    /// "Later" on a recommended update holds for the rest of the process.
    @State private var laterChosen = false
    @Environment(\.scenePhase) private var scenePhase
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
    @State private var pushAsk = false
    @State private var lng: Double = 0

    private var destination: Destination {
        Destination(rawValue: destinationRaw) ?? .meals
    }

    private var authorised: Bool { sessionState is SessionStateAuthenticated }

    private var updateRequired: Bool { update?.need.code == "REQUIRED" }

    /// Opens the App Store. A required update keeps its full-screen gate up,
    /// so returning without updating lands on the gate again.
    private func openStore() {
        guard let current = update else { return }
        if let url = URL(string: current.storeUrl), !current.storeUrl.isEmpty {
            UIApplication.shared.open(url)
        }
        if current.need.code != "REQUIRED" { update = nil }
    }

    /// Launch and every return to the foreground: an app left in the
    /// background for days must still see a raised minimum, and a check that
    /// failed offline gets another go. The SDK keeps the answer for 300s.
    @MainActor
    private func checkForUpdate() async {
        guard let sdk = env.sdk else { return }
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        guard let found = try? await sdk.checkForUpdate(platform: "ios", currentVersion: version) else { return }
        switch found.need.code {
        case "REQUIRED": update = found
        case "RECOMMENDED": update = laterChosen ? nil : found
        default: update = nil
        }
    }

    /// Universal Links and push taps: open the matching meals screen. A push
    /// link the app has no screen for opens on the web instead of being
    /// dropped; a Universal Link is never sent back out (it would loop).
    private func openLink(_ url: String, fromPush: Bool = false) {
        guard let target = MealsLinks.shared.parse(url: url) else {
            if fromPush { openOnWeb(url) }
            return
        }
        if target.page == .support {
            if authorised {
                openHelp(target.id)
            } else {
                // Same "sign in to get help" gate as the Meals banner.
                let id = target.id
                route = .none
                destinationRaw = Destination.meals.rawValue
                pendingMealsHelp = true
                pendingMeals = { openHelp(id, switchToMe: false) }
            }
            return
        }
        route = .none
        // Both meals tabs host MealsScreen, which follows the link itself; a
        // tab that can take it keeps it (see MainActivity on Android).
        if destinationRaw != Destination.myMeals.rawValue { destinationRaw = Destination.meals.rawValue }
        link = target
    }

    /// Help is native: a ticket (or the Help list) on the app's own session.
    /// It is an overlay, so from a Meals banner it opens over Meals and Back
    /// returns there; only a push or deep link moves to Me.
    private func openHelp(_ ticketId: String?, switchToMe: Bool = true) {
        if switchToMe { destinationRaw = Destination.me.rawValue }
        if let ticketId {
            supportViewModel?.openTicket(id: ticketId)
            route = .helpTicket
        } else {
            route = .help
        }
    }

    private func openOnWeb(_ target: String) {
        let full = target.hasPrefix("/") ? Config.webBaseUrl + target : target
        guard full.hasPrefix("https://") || full.hasPrefix("http://"),
              let url = URL(string: full) else { return }
        UIApplication.shared.open(url)
    }
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
                ], requireAccount: requireAccount)
                    .opacity(destination == .feed ? 1 : 0)

                if destination != .feed {
                    tab(vm: vm, me: me)
                        .environment(\.colorScheme, .light)
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
                    // Opaque to touch: an Inbox/Thread area with no control
                    // (header gaps, a short list) must not pass the tap to
                    // the feed's Sell/Search icons underneath.
                    messaging(me: me)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                        .background(Palette.ink.ignoresSafeArea())
                        .contentShape(Rectangle())
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
            if supportViewModel == nil { supportViewModel = sdk.createSupportViewModel() }
            if destination != .feed { feedViewModel?.onAppBackgrounded() }
            session = sdk.session
            await checkForUpdate()
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await checkForUpdate() } }
        }
        .task(id: session != nil) {
            guard let session else { return }
            for await state in session.state {
                sessionState = state
                if state is SessionStateAuthenticated {
                    // The system prompt only after a sentence saying what it
                    // is for, and only while the person has not answered it.
                    if await PushDelegate.shouldAsk() { pushAsk = true } else { PushDelegate.registerIfAllowed() }
                }
            }
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
                supportViewModel?.forget()
                sellViewModel = nil
                pendingFeed = nil
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
            guard ok else { return }
            if let run = pendingFeed {
                pendingFeed = nil
                destinationRaw = Destination.feed.rawValue
                run()
            }
            guard let run = pendingMeals else { return }
            pendingMeals = nil
            pendingMealsHelp = false
            run()
        }
        .onChange(of: destination) { _, new in
            // The sell flow's state holder is kept across a tab change (a deep
            // link, a push tap) so a recorded clip and its answers survive; it
            // is released when the flow is left or finished.

            // Video stops when the viewer leaves the feed. Without this the reel
            // keeps decoding — and keeps its audio — under a screen that is not
            // showing it: a battery cost, and watch time credited to a reel
            // nobody is watching.
            guard let vm = feedViewModel else { return }
            if new == .feed { vm.onAppForegrounded() } else { vm.onAppBackgrounded() }
        }
        .onOpenURL { openLink($0.absoluteString) }
        .onChange(of: scenePhase) { _, phase in
            // A push token that failed to reach the server gets another go.
            if phase == .active { env.sdk?.retryPushToken() }
        }
        .alert(Text("push_rationale_title"), isPresented: $pushAsk) {
            Button { PushDelegate.requestPermission() } label: { Text("push_rationale_allow") }
            Button(role: .cancel) {} label: { Text("push_rationale_later") }
        } message: {
            Text("push_rationale_body")
        }
        .task { PushDelegate.onLink = { openLink($0, fromPush: true) } }
        // Recommended only: dismissible, so an alert is right. A required
        // update is the full-screen gate below, which no tap can clear.
        .alert(
            Text("update_title"),
            isPresented: Binding(get: { update != nil && !updateRequired }, set: { if !$0 { update = nil } })
        ) {
            Button("update_action") { openStore() }
            Button("update_later", role: .cancel) { laterChosen = true; update = nil }
        } message: {
            Text("update_body")
        }
        .overlay {
            if updateRequired {
                UpdateRequiredGate(onUpdate: openStore)
            }
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
            ProgressView().tint(Palette.accent)
        } else if needsAccount && !authorised, let gate = signInViewModel {
            // Saved and Sell open from the feed, so cancelling returns there.
            SignInGate(viewModel: gate, onCancel: {
                gate.reset()
                pendingFeed = nil
                destinationRaw = (destination == .saved || destination == .sell)
                    ? Destination.feed.rawValue : Destination.meals.rawValue
            }, titleKey: gateCopy.0, whyKey: gateCopy.1)
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
                                if authorised { run() } else { pendingMealsHelp = false; pendingMeals = run }
                            },
                            startOnMine: destination == .myMeals,
                            link: link,
                            onLinkHandled: { link = nil },
                            onOpenHelp: { ticketId in
                                if authorised {
                                    openHelp(ticketId, switchToMe: false)
                                } else {
                                    pendingMealsHelp = true
                                    pendingMeals = { openHelp(ticketId, switchToMe: false) }
                                }
                            }
                        )
                        .id(destination)
                        if pendingMeals != nil && !authorised, let gate = signInViewModel {
                            SignInGate(viewModel: gate, onCancel: { pendingMeals = nil; pendingMealsHelp = false },
                                       titleKey: pendingMealsHelp ? "help_signin_title" : "meals_signin_title",
                                       whyKey: pendingMealsHelp ? "help_signin_why" : "meals_signin_why")
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
                        requireAccount {
                            me.inquire(listingId: result.id, title: result.title,
                                       reelId: nil, actionId: nil)
                            route = .thread
                        }
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
                    },
                    makeAccount: { env.sdk?.createAccountViewModel() },
                    onOpenHelp: { route = .help },
                    onOpenInvoices: { route = .invoices }
                )
            case .sell:
                // Its state holder is created on entry and released on exit
                // rather than living for the process: it holds the recorded
                // clip in memory, and twenty megabytes kept alive after a
                // seller abandoned the flow is twenty megabytes on the device
                // class least able to spare it.
                SellFlow(
                    viewModel: sellViewModel ?? makeSellViewModel(),
                    onLeave: {
                        sellViewModel = nil
                        destinationRaw = Destination.feed.rawValue
                    },
                    onSeeFeed: {
                        sellViewModel = nil
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
        case .help:
            if let support = supportViewModel {
                HelpScreen(viewModel: support, onBack: { route = .none }, onOpenTicket: { id in
                    support.openTicket(id: id)
                    route = .helpTicket
                })
            }
        case .helpTicket:
            if let support = supportViewModel {
                HelpTicketScreen(viewModel: support, onBack: { route = .help })
            }
        case .invoices:
            if let support = supportViewModel {
                InvoicesScreen(viewModel: support, onBack: { route = .none }, onOpenInvoice: { id in
                    support.openInvoice(id: id)
                    route = .invoiceDetail
                })
            }
        case .invoiceDetail:
            if let support = supportViewModel {
                InvoiceDetailScreen(viewModel: support, onBack: { route = .invoices })
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

    /// Runs `run` now when signed in; otherwise keeps it and sends the viewer
    /// to sign in, replaying it from `onChange(of: authorised)`.
    private func requireAccount(_ run: @escaping () -> Void) {
        if authorised { run() } else {
            pendingFeed = run
            destinationRaw = Destination.me.rawValue
        }
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
            requireAccount {
                me.inquire(listingId: targetId, title: title, reelId: reelId, actionId: action.id)
                route = .thread
            }
        default:
            break
        }
    }
}

/// Blocks the app below the minimum supported version. A view rather than an
/// alert: SwiftUI dismisses an alert on any button tap, so a "required" alert
/// can't be relied on to come back after a trip to the App Store.
private struct UpdateRequiredGate: View {
    let onUpdate: () -> Void

    var body: some View {
        VStack(spacing: Space.large) {
            Spacer()
            Text("update_required_title")
                .font(.system(size: TypeScale.title, weight: .semibold))
                .foregroundStyle(Palette.text)
                .multilineTextAlignment(.center)
            Text("update_required_body")
                .font(.system(size: TypeScale.body))
                .foregroundStyle(Palette.textMuted)
                .multilineTextAlignment(.center)
            Button(action: onUpdate) {
                Text("update_action")
                    .font(.system(size: TypeScale.body, weight: .semibold))
                    .foregroundStyle(Color.white)
                    .frame(maxWidth: .infinity, minHeight: Space.touch)
                    .background(Palette.accent, in: RoundedRectangle(cornerRadius: 12))
            }
            Spacer()
        }
        .padding(.horizontal, Space.xl)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Palette.ink.ignoresSafeArea())
        .contentShape(Rectangle())
    }
}
