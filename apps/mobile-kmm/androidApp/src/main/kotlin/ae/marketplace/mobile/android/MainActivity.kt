package ae.marketplace.mobile.android

import ae.marketplace.mobile.feed.FeedViewModel
import android.os.Bundle
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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {

    private var viewModel: FeedViewModel? = null

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
                    created.start(lat = 25.0805, lng = 55.1403)
                    vm = created
                }

                Box(
                    modifier = Modifier.fillMaxSize().background(Color.Black),
                    contentAlignment = Alignment.Center,
                ) {
                    val current = vm
                    if (current == null) {
                        CircularProgressIndicator()
                    } else {
                        ReelFeed(viewModel = current)
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
