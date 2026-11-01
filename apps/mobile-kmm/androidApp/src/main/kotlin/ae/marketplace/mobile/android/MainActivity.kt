package ae.marketplace.mobile.android

import androidx.compose.runtime.key
import androidx.compose.ui.res.stringResource
import ae.marketplace.mobile.android.me.InboxScreen
import ae.marketplace.mobile.android.me.MeScreen
import ae.marketplace.mobile.android.me.SavedScreen
import ae.marketplace.mobile.android.me.SearchScreen
import ae.marketplace.mobile.android.me.ThreadScreen
import ae.marketplace.mobile.android.sell.SellFlow
import ae.marketplace.mobile.android.sell.SignInGate
import ae.marketplace.mobile.android.meals.MealsTab
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.meals.MealsViewModel
import ae.marketplace.mobile.model.LatLng
import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.identity.Session
import ae.marketplace.mobile.identity.SessionEnded
import ae.marketplace.mobile.identity.SessionState
import ae.marketplace.mobile.identity.SignInViewModel
import ae.marketplace.mobile.identity.isAuthenticated
import ae.marketplace.mobile.identity.isSettled
import kotlinx.coroutines.flow.MutableStateFlow
import ae.marketplace.mobile.me.MeViewModel
import ae.marketplace.mobile.sell.SellViewModel
import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.location.LocationManager
import android.os.Bundle
import android.widget.Toast
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.AddCircleOutline
import androidx.compose.material.icons.rounded.BookmarkBorder
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.launch

/**
 * What is drawn *over* the tabs.
 *
 * Messaging is not a destination. It is reached from the Me screen and from a
 * CTA press three swipes into the feed, and making it a sixth tab would turn
 * the second one into a context switch out of the reel the viewer is watching.
 */
private enum class Route { None, Inbox, Thread }

class MainActivity : ComponentActivity() {

    private var viewModel: FeedViewModel? = null

    /**
     * Dubai Marina.
     *
     * The API requires a point — "a feed without a location is a different
     * product" — so there is no "everywhere" to fall back to. A fixed point is
     * more honest than a silent nationwide feed, and the filter sheet always
     * shows which one is in use.
     */
    private val fallback = Place("Dubai Marina", 25.0805, 55.1403, R.string.place_dubai_marina)

    /**
     * Registered here rather than inside composition: a permission result can
     * arrive after a configuration change, and a launcher owned by a composable
     * that has since been recomposed away drops it.
     */
    private val locationPermission = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted ->
        val vm = viewModel ?: return@registerForActivityResult
        if (granted) {
            useDeviceLocation(vm)
        } else {
            // Declined is a decision, not an error. The feed keeps working at
            // the fallback and says which one it is using — nagging on every
            // open would be the worse product.
            Toast.makeText(this, R.string.filter_location_denied, Toast.LENGTH_SHORT).show()
            vm.onLocationSelected(fallback.lat, fallback.lng, fallback.label)
        }
    }

    private fun requestLocation(vm: FeedViewModel) {
        val granted = ContextCompat.checkSelfPermission(
            this, Manifest.permission.ACCESS_COARSE_LOCATION
        ) == PackageManager.PERMISSION_GRANTED

        if (granted) useDeviceLocation(vm)
        else locationPermission.launch(Manifest.permission.ACCESS_COARSE_LOCATION)
    }

    /**
     * Last known fix only — no active request.
     *
     * The feed needs a district, not a doorstep, and waiting on a GPS lock for a
     * screen that could already be showing video is how a front door feels slow.
     * A device with no cached fix falls back rather than spinning.
     */
    @SuppressLint("MissingPermission")
    private fun useDeviceLocation(vm: FeedViewModel) {
        val manager = getSystemService(Context.LOCATION_SERVICE) as? LocationManager
        val fix = manager?.let { m ->
            m.getProviders(true).asSequence()
                .mapNotNull { runCatching { m.getLastKnownLocation(it) }.getOrNull() }
                .maxByOrNull { it.time }
        }
        if (fix != null) {
            // Empty label, deliberately: there is no reverse geocoder here and
            // "25.08, 55.14" is not a place anyone recognises. The sheet renders
            // an empty label as "Use my location", which is what it is.
            vm.onLocationSelected(fix.latitude, fix.longitude, "")
        } else {
            vm.onLocationSelected(fallback.lat, fallback.lng, fallback.label)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        setContent {
            MaterialTheme(colorScheme = darkColorScheme()) {
                var vm by remember { mutableStateOf<FeedViewModel?>(null) }

                LaunchedEffect(Unit) {
                    val sdk = (application as MarketplaceApp).sdk.await()
                    val created = sdk.createFeedViewModel()
                    viewModel = created
                    // Dubai Marina, hard-coded until the location permission
                    // flow lands. The API requires lat/lng — "a feed without a
                    // location is a different product" — so there is no
                    // meaningful screen to show without one, and a fixed point
                    // is more honest than a silent nationwide feed.
                    created.start(
                        lat = fallback.lat,
                        lng = fallback.lng,
                        locationLabel = fallback.label,
                    )
                    vm = created
                }

                // rememberSaveable, not remember: a rotation must not drop the
                // viewer back to the feed from wherever they were.
                var destination by rememberSaveable { mutableStateOf(Destination.Meals) }
                var filtersOpen by remember { mutableStateOf(false) }

                // Saved, Me, Search and messaging share one state holder — see
                // MeViewModel. Created once here rather than per tab, because
                // its whole design is that flicking between those tabs does not
                // re-fetch.
                var meVm by remember { mutableStateOf<MeViewModel?>(null) }
                var mealsVm by remember { mutableStateOf<MealsViewModel?>(null) }
                // A meals action waiting on sign-in: checkout, rotation, My meals.
                var pendingMeals by remember { mutableStateOf<(() -> Unit)?>(null) }
                var signInVm by remember { mutableStateOf<SignInViewModel?>(null) }
                var session by remember { mutableStateOf<Session?>(null) }

                // Messaging is an overlay rather than a tab. It is reached from
                // two places that are not each other — the Me screen and a CTA
                // press three swipes into the feed — and a tab would make the
                // second one a context switch out of the feed the viewer is
                // still watching.
                var route by remember { mutableStateOf(Route.None) }

                LaunchedEffect(Unit) {
                    val sdk = (application as MarketplaceApp).sdk.await()
                    meVm = sdk.createMeViewModel().also {
                        it.setLocation(fallback.lat, fallback.lng)
                    }
                    mealsVm = sdk.createMealsViewModel()
                    signInVm = sdk.createSignInViewModel()
                    session = sdk.session
                }

                // The one answer to "is there an account behind this session".
                //
                // Replaces a `rememberSaveable` boolean that started `false`,
                // which asked every returning viewer to sign in again while
                // their refresh token sat unread on disk. Session.Unknown is
                // why this is not simply a Boolean — see that class.
                val sessionState by (session?.state ?: remember {
                    MutableStateFlow<SessionState>(SessionState.Unknown)
                }).collectAsState()
                val authorised = sessionState.isAuthenticated

                // A session that ends underneath the viewer takes its screens
                // with it. Without the `forget`, the next person to sign in on
                // this device saw the previous account's inbox — the state
                // holder outlives the session by design, and its `loaded` flags
                // meant it would never re-fetch to correct itself.
                val ended by (session?.ended ?: remember {
                    MutableStateFlow<SessionEnded?>(null)
                }).collectAsState()

                LaunchedEffect(ended) {
                    val why = ended ?: return@LaunchedEffect
                    meVm?.forget()
                    mealsVm?.forget()
                    if (why == SessionEnded.Expired) {
                        // Only for the involuntary case. Telling someone who
                        // just tapped Sign out that they have been signed out is
                        // noise; telling someone whose token was revoked
                        // mid-scroll is the difference between understanding and
                        // assuming the app is broken.
                        Toast.makeText(
                            this@MainActivity,
                            R.string.session_expired,
                            Toast.LENGTH_LONG,
                        ).show()
                        if (destination != Destination.Feed && destination != Destination.Search && destination != Destination.Meals) {
                            destination = Destination.Meals
                        }
                    }
                    session?.clearEnded()
                }

                Box(
                    modifier = Modifier.fillMaxSize().background(Color.Black),
                    contentAlignment = Alignment.Center,
                ) {
                    val current = vm
                    if (current == null) {
                        CircularProgressIndicator()
                    } else {
                        // The feed stays composed across tab changes rather than
                        // being swapped out. Recreating it would drop the pager
                        // position, the loaded pages and the decoder pool, so
                        // coming back from Saved would mean a fresh network page
                        // and a viewer who lost their place — and the feed is the
                        // one screen where losing your place is the whole cost.
                        Box(Modifier.fillMaxSize().alpha(if (destination == Destination.Feed) 1f else 0f)) {
                            ReelFeed(
                                viewModel = current,
                                onOpenFilters = { filtersOpen = true },
                                extras = listOf(
                                    FeedExtra(stringResource(R.string.nav_search), Icons.Rounded.Search) { destination = Destination.Search },
                                    FeedExtra(stringResource(R.string.nav_saved), Icons.Rounded.BookmarkBorder) { destination = Destination.Saved },
                                    FeedExtra(stringResource(R.string.nav_sell), Icons.Rounded.AddCircleOutline) { destination = Destination.Sell },
                                ),
                                onActionPressed = { action, reelId ->
                                    // The branch is on `kind`, never on
                                    // `target_type`. That distinction is the
                                    // whole action seam: a vertical adding a
                                    // new *target* costs nothing here, and the
                                    // set of things a client can physically do
                                    // — open a chat, start a checkout, follow
                                    // someone — is genuinely closed and shipped
                                    // in the app.
                                    //
                                    // Anything unrecognised falls through
                                    // having recorded the click, which is what
                                    // the app did for every CTA until now. A
                                    // newer server's action must not crash an
                                    // older build.
                                    when (action.kind) {
                                        "inquire_listing" -> {
                                            val listingId = action.targetId
                                            if (listingId == null) return@ReelFeed
                                            if (!authorised) {
                                                // A message needs an account,
                                                // and this is the first moment
                                                // it genuinely does. Asking here
                                                // rather than at launch is the
                                                // same call the Sell tab makes.
                                                destination = Destination.Me
                                                return@ReelFeed
                                            }
                                            meVm?.inquire(
                                                listingId = listingId,
                                                title = current.state.value.items
                                                    .firstOrNull { it.id == reelId }
                                                    ?.summary?.title.orEmpty(),
                                                reelId = reelId,
                                                actionId = action.id,
                                            )
                                            route = Route.Thread
                                        }
                                    }
                                },
                            )
                        }

                        if (filtersOpen) {
                            val state by current.state.collectAsState()
                            FilterSheet(
                                state = state,
                                onVertical = current::onVerticalSelected,
                                onPlace = { current.onLocationSelected(it.lat, it.lng, it.label) },
                                onUseMyLocation = { requestLocation(current) },
                                onDismiss = { filtersOpen = false },
                            )
                        }
                        val me = meVm
                        val gate = signInVm

                        // Three of the five need an account. Search does not —
                        // it is the highest-intent surface in the product and
                        // the one most likely to arrive from a shared link, and
                        // a login wall there converts a motivated buyer into a
                        // bounce. The same argument the API makes by leaving
                        // GET /listings public.
                        // Where the feed is pointed. Read here rather than in
                        // each screen so there is one source of truth for "where
                        // the viewer is" — search measures distance from it, and
                        // the Nearest sort is only offered when it exists.
                        val feedState by current.state.collectAsState()
                        LaunchedEffect(feedState.lat, feedState.lng, me) {
                            if (feedState.lat != 0.0 || feedState.lng != 0.0) {
                                me?.setLocation(feedState.lat, feedState.lng)
                            }
                        }

                        val needsAccount = destination in
                            setOf(Destination.Sell, Destination.Saved, Destination.Me, Destination.MyMeals)

                        // Search, Saved and Sell are reached from the feed, so Back returns there.
                        BackHandler(enabled = destination in setOf(Destination.Search, Destination.Saved, Destination.Sell)) {
                            destination = Destination.Feed
                        }

                        if (needsAccount && !sessionState.isSettled) {
                            // The token store is still being read. Neither the
                            // gate nor the account screen is the right thing to
                            // draw yet, and picking one produces a frame of the
                            // wrong screen for a returning viewer. It is a few
                            // milliseconds; a spinner is the honest answer.
                            CircularProgressIndicator()
                        } else if (needsAccount && !authorised && gate != null) {
                            // Saved and Sell open from the feed, so cancelling returns there.
                            val closeGate = {
                                gate.reset()
                                destination = if (destination == Destination.Saved || destination == Destination.Sell) {
                                    Destination.Feed
                                } else {
                                    Destination.Meals
                                }
                            }
                            BackHandler(onBack = closeGate)
                            SignInGate(
                                viewModel = gate,
                                // Nothing to set. The gate reports to Session,
                                // and this composition is already observing it —
                                // a local flag here is what let the two disagree.
                                onCancel = closeGate,
                                title = when (destination) {
                                    Destination.MyMeals -> R.string.meals_signin_mine_title
                                    Destination.Me -> R.string.me_signin_title
                                    Destination.Saved -> R.string.saved_signin_title
                                    else -> R.string.signin_title
                                },
                                why = when (destination) {
                                    Destination.MyMeals -> R.string.meals_signin_why
                                    Destination.Me -> R.string.me_signin_why
                                    Destination.Saved -> R.string.saved_signin_why
                                    else -> R.string.signin_why
                                },
                                light = destination == Destination.MyMeals,
                            )
                        } else when (destination) {
                            Destination.Feed -> Unit // drawn above, always composed

                            Destination.Meals, Destination.MyMeals -> mealsVm?.let { key(destination) {
                                LaunchedEffect(authorised) {
                                    if (authorised) pendingMeals?.let { run -> pendingMeals = null; run() }
                                }
                                val here = if (feedState.lat != 0.0 || feedState.lng != 0.0) {
                                    LatLng(feedState.lat, feedState.lng)
                                } else {
                                    LatLng(fallback.lat, fallback.lng)
                                }
                                // The gate sits over the tab rather than replacing it, so
                                // the kitchen and plan the viewer picked survive sign-in.
                                Box(Modifier.fillMaxSize()) {
                                    MealsTab(
                                        viewModel = it,
                                        location = here,
                                        startOnMine = destination == Destination.MyMeals,
                                        requireAccount = { run -> if (authorised) run() else pendingMeals = run },
                                    )
                                    if (pendingMeals != null && !authorised && gate != null) {
                                        // Composed after MealsTab, so Back closes the gate
                                        // instead of popping the page underneath it.
                                        BackHandler { gate.reset(); pendingMeals = null }
                                        SignInGate(
                                            viewModel = gate,
                                            onCancel = { gate.reset(); pendingMeals = null },
                                            modifier = Modifier.fillMaxSize(),
                                            title = R.string.meals_signin_title,
                                            why = R.string.meals_signin_why,
                                            light = true,
                                        )
                                    }
                                }
                            } }

                            Destination.Search -> me?.let {
                                SearchScreen(
                                    viewModel = it,
                                    // Gates the "Nearest" sort. The server falls
                                    // back to newest without a point, and a sort
                                    // that looks selected while producing a
                                    // different order is worse than one that is
                                    // not offered.
                                    hasLocation = feedState.lat != 0.0 || feedState.lng != 0.0,
                                    // The video first when there is one.
                                    //
                                    // This went straight to a conversation for
                                    // every result, which skipped the reel
                                    // entirely — making the highest-intent
                                    // surface in the product the one place you
                                    // could not watch anything. A buyer wants to
                                    // see the car before they message about it,
                                    // and the reel carries its own CTA to the
                                    // same thread.
                                    //
                                    // Listings created on the web have no reel,
                                    // and those still open a conversation: a
                                    // search result with no way to act on it is
                                    // a catalogue, and this product's whole
                                    // conversion event is the message.
                                    onOpen = { result ->
                                        // The keyboard goes with the screen.
                                        // Leaving it up over a video is the
                                        // kind of thing that looks like the tap
                                        // did not register.
                                        val reel = result.reelId
                                        when {
                                            reel != null -> current.openReel(reel)
                                            !authorised -> destination = Destination.Me
                                            else -> {
                                                it.inquire(result.id, result.title, null, null)
                                                route = Route.Thread
                                            }
                                        }
                                    },
                                )
                            }

                            Destination.Saved -> me?.let {
                                SavedScreen(
                                    viewModel = it,
                                    // The reel they kept, not the feed in
                                    // general. This was the dead end that made
                                    // the whole tab decorative: a bookmark that
                                    // opens something else is the least useful
                                    // possible answer to a bookmark.
                                    onOpen = { saved -> current.openReel(saved.reelId) },
                                )
                            }

                            Destination.Me -> me?.let {
                                MeScreen(
                                    viewModel = it,
                                    onOpenInbox = { route = Route.Inbox },
                                    onSignOut = {
                                        // Clears the token store, which the old
                                        // flag-flip did not — the credential
                                        // stayed on the device and a relaunch
                                        // would have handed the next person a
                                        // live session. `forget` happens in the
                                        // `ended` effect, for both exits.
                                        lifecycleScope.launch { session?.signOut() }
                                        destination = Destination.Feed
                                    },
                                )
                            }

                            // The Sell tab holds a recorded clip in memory, so
                            // its state holder is created on entry and released
                            // on exit rather than living for the process.
                            Destination.Sell -> {
                                var sellVm by remember { mutableStateOf<SellViewModel?>(null) }
                                LaunchedEffect(Unit) {
                                    sellVm = (application as MarketplaceApp).sdk.await().createSellViewModel()
                                }
                                sellVm?.let { vm ->
                                    SellFlow(
                                        viewModel = vm,
                                        onLeave = { destination = Destination.Feed },
                                        onSeeFeed = {
                                            destination = Destination.Feed
                                            // The seller's own reel is new supply
                                            // at this location, and the feed they
                                            // are looking at was built before it
                                            // existed. Refreshing is the
                                            // difference between "See it in the
                                            // feed" showing it and sending them
                                            // to look for nothing.
                                            current.refresh()
                                        },
                                    )
                                }
                            }
                        }

                        // One reel, over the tabs and under messaging.
                        //
                        // Under, because its CTA opens a thread and the thread
                        // has to be on top of the reel that produced it. Over
                        // the tabs, because the feed underneath must keep its
                        // pager position — dismissing this returns the viewer to
                        // where they were, not to the top of a new page.
                        ReelDetail(
                            viewModel = current,
                            returnToIndex = 0,
                            onActionPressed = { action, reelId ->
                                if (action.kind == "inquire_listing" || action.kind == "enquire_property") {
                                    val targetId = action.targetId ?: return@ReelDetail
                                    if (!authorised) {
                                        destination = Destination.Me
                                        return@ReelDetail
                                    }
                                    meVm?.inquire(
                                        listingId = targetId,
                                        title = current.focused.value.item?.summary?.title.orEmpty(),
                                        reelId = reelId,
                                        actionId = action.id,
                                    )
                                    route = Route.Thread
                                }
                            },
                            onClose = {},
                        )

                        // Messaging, over everything.
                        if (me != null && route != Route.None) {
                            when (route) {
                                Route.Inbox -> InboxScreen(
                                    viewModel = me,
                                    onOpen = { c ->
                                        me.openThread(c.id, c.listingTitle)
                                        route = Route.Thread
                                    },
                                    onBack = { route = Route.None },
                                )
                                Route.Thread -> ThreadScreen(
                                    viewModel = me,
                                    onBack = {
                                        me.closeThread()
                                        // Back to the inbox rather than to
                                        // wherever the thread was opened from.
                                        // A buyer who messaged from the feed has
                                        // just acquired an inbox, and this is the
                                        // moment they learn it exists.
                                        route = Route.Inbox
                                    },
                                )
                                Route.None -> Unit
                            }
                        }

                        // Video stops when the viewer leaves the feed. Without
                        // this the reel keeps decoding — and keeps its audio —
                        // underneath a screen that is not showing it, which is
                        // both a battery cost and watch time credited to a reel
                        // nobody is watching.
                        LaunchedEffect(destination) {
                            if (destination == Destination.Feed) current.onAppForegrounded()
                            else current.onAppBackgrounded()
                        }
                    }

                    // Hidden during the sell flow. Every step past the
                    // viewfinder ends in a full-width button, and a floating
                    // nav bar over "Publish" is a mis-tap that either loses the
                    // seller's work or publishes it early. Sell is modal: it
                    // leaves by its own ✕ or Done.
                    // Hidden during the sell flow, in messaging, and behind a
                    // focused reel — all three are modal, and a floating nav bar
                    // over a modal is a mis-tap that loses whatever the viewer was
                    // doing.
                    val focusedOpen = vm?.focused?.collectAsState()?.value?.open == true
                    if (destination != Destination.Sell && route == Route.None && !focusedOpen) {
                        BottomNav(
                            selected = destination,
                            onSelect = { destination = it },
                            modifier = Modifier.align(Alignment.BottomCenter),
                        )
                    }
                }
            }
        }

        // Playback follows the lifecycle rather than composition. A reel that
        // keeps decoding behind the lock screen burns battery and, worse, keeps
        // reporting watch time for a screen nobody is looking at.
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.RESUMED) {
                viewModel?.onAppForegrounded()
                try {
                    kotlinx.coroutines.awaitCancellation()
                } finally {
                    viewModel?.onAppBackgrounded()
                }
            }
        }
    }
}
