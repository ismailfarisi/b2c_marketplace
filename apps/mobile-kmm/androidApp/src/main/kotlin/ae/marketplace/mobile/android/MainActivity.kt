package ae.marketplace.mobile.android

import ae.marketplace.mobile.android.sell.SellFlow
import ae.marketplace.mobile.android.sell.SignInGate
import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.identity.SignInViewModel
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
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
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
    private val fallback = Place("Dubai Marina", 25.0805, 55.1403)

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
                var destination by rememberSaveable { mutableStateOf(Destination.Feed) }
                var filtersOpen by remember { mutableStateOf(false) }

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
                            ReelFeed(viewModel = current, onOpenFilters = { filtersOpen = true })
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
                        // The Sell tab is its own flow with its own state
                        // holder, created on first entry rather than at launch:
                        // it holds a recorded clip in memory, and most sessions
                        // never open it.
                        if (destination == Destination.Sell) {
                            var sellVm by remember { mutableStateOf<SellViewModel?>(null) }
                            var signInVm by remember { mutableStateOf<SignInViewModel?>(null) }
                            // Held here rather than read from the sign-in state
                            // each recomposition, so that completing the gate
                            // moves the seller on exactly once.
                            var authorised by rememberSaveable { mutableStateOf(false) }

                            LaunchedEffect(Unit) {
                                val sdk = (application as MarketplaceApp).sdk.await()
                                signInVm = sdk.createSignInViewModel()
                                sellVm = sdk.createSellViewModel()
                            }

                            val gate = signInVm
                            if (!authorised && gate != null) {
                                SignInGate(
                                    viewModel = gate,
                                    onSignedIn = { authorised = true },
                                    onCancel = { destination = Destination.Feed },
                                )
                            }
                            sellVm?.takeIf { authorised }?.let { vm ->
                                SellFlow(
                                    viewModel = vm,
                                    onLeave = { destination = Destination.Feed },
                                    onSeeFeed = {
                                        destination = Destination.Feed
                                        // The seller's own reel is new supply at
                                        // this location, and the feed they are
                                        // looking at was built before it existed.
                                        // Refreshing is the difference between
                                        // "See it in the feed" showing it and
                                        // sending them to look for nothing.
                                        current.refresh()
                                    },
                                )
                            }
                        } else if (destination != Destination.Feed) {
                            NotBuiltYet(destination)
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
                    if (destination != Destination.Sell) {
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
