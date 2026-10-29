package ae.marketplace.mobile

import ae.marketplace.mobile.events.EventQueue
import ae.marketplace.mobile.events.EventSender
import ae.marketplace.mobile.feed.FeedApi
import ae.marketplace.mobile.feed.FeedPager
import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.identity.Session
import ae.marketplace.mobile.identity.SignInApi
import ae.marketplace.mobile.me.MeApi
import ae.marketplace.mobile.meals.MealsApi
import ae.marketplace.mobile.meals.MealsViewModel
import ae.marketplace.mobile.me.MeViewModel
import ae.marketplace.mobile.identity.SignInViewModel
import ae.marketplace.mobile.identity.ViewerIdentity
import ae.marketplace.mobile.identity.deviceIdProvider
import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.TokenStore
import ae.marketplace.mobile.network.tokenStore
import ae.marketplace.mobile.playback.PlaybackCoordinator
import ae.marketplace.mobile.playback.ReelPlayerPool
import ae.marketplace.mobile.playback.createReelPlayer
import ae.marketplace.mobile.sell.SellApi
import ae.marketplace.mobile.sell.SellViewModel
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.Dispatchers
// kotlinx-datetime 0.7 handed Clock back to the stdlib. `kotlinx.datetime.Clock`
// is gone, and the replacement is still experimental — opted into here in one
// place rather than at every call site.
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

/**
 * The one object each app constructs at launch.
 *
 * Hand-wired rather than a DI framework. The graph is nine objects and it is
 * read from top to bottom in forty lines; Koin or Hilt would add a runtime, a
 * module DSL and a class of error that only appears on a device, in exchange for
 * saving those forty lines. The Go side made the same call in
 * `internal/app/BuildRegistries` and for the same reason.
 *
 * Constructed with [create] rather than at the declaration site so both apps get
 * an identical graph — the failure this prevents is the one `internal/app`
 * exists to prevent too: one platform quietly ending up with a stub where the
 * other has the real thing.
 */
@OptIn(ExperimentalTime::class)
class MarketplaceSdk private constructor(
    val identity: ViewerIdentity,
    val tokens: TokenStore,
    /**
     * Whether there is an account behind this session.
     *
     * Owned here rather than by a view model, because three view models and the
     * shell all need the same answer and a session that ends must end for all
     * of them at once. See [Session] for what the per-screen boolean it replaced
     * got wrong.
     */
    val session: Session,
    private val context: PlatformContext,
    private val api: FeedApi,
    private val sell: SellApi,
    private val signIn: SignInApi,
    private val me: MeApi,
    private val meals: MealsApi,
    private val scope: CoroutineScope,
    private val network: NetworkStatus,
) {
    companion object {
        suspend fun create(
            context: PlatformContext,
            baseUrl: String,
            locale: String = "en",
            sessionId: String,
        ): MarketplaceSdk {
            val store = tokenStore(context)
            val deviceId = deviceIdProvider(context).deviceId()
            val identity = ViewerIdentity(deviceId = deviceId, locale = locale)
            val session = Session(store)

            val client = ApiClientFactory.create(
                baseUrl, store, identity,
                onAuthLost = session::expired,
            )
            val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

            // Read the store before returning, so the first composition already
            // knows. Doing it lazily would put the shell back where it was: a
            // frame of the sign-in gate in front of a viewer who is signed in.
            session.restore()

            return MarketplaceSdk(
                identity = identity,
                tokens = store,
                session = session,
                context = context,
                api = FeedApi(client),
                // Two clients, and the second one is not a duplicate — see
                // ApiClientFactory.createUploader. Built once here rather than
                // per sell session so a seller who abandons and restarts does
                // not leak an engine each time.
                sell = SellApi(client, ApiClientFactory.createUploader()),
                signIn = SignInApi(client, store),
                me = MeApi(client),
                meals = MealsApi(client),
                scope = scope,
                network = networkStatus(context),
            ).also { it.sessionId = sessionId }
        }
    }

    private var sessionId: String = ""

    /**
     * Builds the feed's state holder and everything under it.
     *
     * A factory rather than a singleton: the coordinator owns decoders, and a
     * feed screen that is left and re-entered must release them. A shared
     * instance would hold hardware decoders for the life of the process, which
     * on a mid-range Android device means the next video anywhere in the app
     * fails to start.
     */
    fun createFeedViewModel(): FeedViewModel {
        val events = EventQueue(
            sender = EventSender { api.submitEvents(it) },
            sessionId = sessionId,
            timestampProvider = { Clock.System.now().toString() },
            scope = scope,
        )

        val playback = PlaybackCoordinator(
            pool = ReelPlayerPool(factory = { createReelPlayer(context) }),
            events = events,
            metered = { network.isMetered() },
            now = { Clock.System.now().toEpochMilliseconds() },
        )

        return FeedViewModel(
            pager = FeedPager(api),
            playback = playback,
            events = events,
            api = api,
        )
    }

    /**
     * Builds the sell flow's state holder.
     *
     * A factory for a reason the feed's comment already gives in a different
     * form: this one holds the recorded clip in memory. A singleton would keep
     * twenty megabytes alive for the life of the process after a seller
     * abandoned the flow, on the device class least able to spare it.
     */
    fun createSellViewModel(): SellViewModel = SellViewModel(sell)

    /**
     * The sign-in gate.
     *
     * Not created at launch. The feed is browsable signed out, so most sessions
     * never need this — and asking a visitor for a phone number before they have
     * seen a single reel is how a discovery app loses the visit.
     */
    fun createSignInViewModel(): SignInViewModel = SignInViewModel(signIn, session)

    /**
     * Saved, Me, Search and messaging.
     *
     * One instance for the life of the shell rather than one per tab, because
     * its whole design is that flicking between those tabs does not re-fetch.
     */
    fun createMeViewModel(): MeViewModel = MeViewModel(me)

    /** The meals tab: storefront, checkout and the customer's subscriptions. */
    fun createMealsViewModel(): MealsViewModel = MealsViewModel(meals)
}
