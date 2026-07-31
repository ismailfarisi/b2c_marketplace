package ae.marketplace.mobile.android

import ae.marketplace.mobile.android.ui.Poster
import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.playback.asExoPlayer
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.common.Player
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import androidx.compose.ui.unit.dp

/**
 * Attaches the shared player to a surface, over the reel's own still.
 *
 * The video integration is short because the parts that are hard — which
 * rendition, how many players, when to prepare — are in `:shared` and already
 * decided by the time this runs. All that is left is handing a `SurfaceView` to
 * a decoder somebody else created.
 *
 * # The poster underneath
 *
 * What this composable used to draw before the first frame decoded was black,
 * and that is worse than it sounds on a vertical pager. A swipe reveals the
 * next reel immediately; the video takes a manifest fetch, a segment fetch and
 * a decode to produce anything. In between, a feed built for browsing looks
 * like a feed that has stopped working — and on a slow connection it stays that
 * way for seconds.
 *
 * So the poster and its blurhash sit behind the player and are removed only
 * when the player reports it has actually rendered a frame. `onRenderedFirstFrame`
 * rather than `isPlaying`: a player can be playing audio, or be buffering with
 * `playWhenReady` set, and still have nothing on the surface. Hiding the poster
 * on the wrong signal reintroduces the black frame in a form that is harder to
 * see because it is now intermittent.
 *
 * It also covers the case that is not a delay at all — a reel whose renditions
 * 404, which in this dev stack is every seeded row. Those used to be a black
 * rectangle with an overlay floating on it, indistinguishable from a bug. Now
 * they are the still, which is what the reel is about.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
fun ReelSurface(
    reelId: String,
    isActive: Boolean,
    viewModel: FeedViewModel,
    posterUrl: String,
    lqip: String,
    modifier: Modifier = Modifier,
) {
    val player = viewModel.playerFor(reelId)?.asExoPlayer()

    // Reset per reel. A pooled player is reused across reels, so without the
    // key this would stay false from the previous occupant and the new reel's
    // poster would never be drawn at all.
    var painted by remember(reelId) { mutableStateOf(false) }

    Box(modifier.background(Color.Black)) {
        if (!painted) {
            Poster(
                url = posterUrl.ifEmpty { null },
                lqip = lqip.ifEmpty { null },
                // Crop, matching the player's RESIZE_MODE_ZOOM. Any other scale
                // here makes the poster jump to a different framing at the
                // moment the first frame lands, which is more distracting than
                // the black it replaced.
                contentScale = ContentScale.Crop,
                corner = 0.dp,
                modifier = Modifier.fillMaxSize(),
            )
        }

        if (player == null) {
            // Outside the prefetch window. Not creating a decoder here is the
            // point of the null: a surface that eagerly made its own player
            // would defeat the pool and the egress policy in one line. The
            // poster above still draws, so the reel is not a black hole while
            // the viewer scrolls towards it.
            return@Box
        }

        DisposableEffect(player, reelId) {
            val listener = object : Player.Listener {
                override fun onRenderedFirstFrame() {
                    painted = true
                }
            }
            player.addListener(listener)
            onDispose { player.removeListener(listener) }
        }

        AndroidView(
            modifier = Modifier.fillMaxSize(),
            factory = { context ->
                PlayerView(context).apply {
                    useController = false
                    // Fill and crop. A vertical reel on a tall phone letterboxed
                    // into a 16:9 box is the single most obvious sign of a feed
                    // built by someone who did not watch it.
                    resizeMode = AspectRatioFrameLayout.RESIZE_MODE_ZOOM
                    // Transparent rather than black: the shutter is what covers
                    // the surface before the first frame, and painting it black
                    // would hide the poster this whole arrangement exists to
                    // show.
                    setShutterBackgroundColor(android.graphics.Color.TRANSPARENT)
                }
            },
            update = { view -> view.player = player },
            onRelease = { view ->
                // Detach, never release. The player belongs to the shared pool
                // and is about to be reused for another reel; releasing it here
                // would destroy a decoder the coordinator still thinks it owns.
                view.player = null
            },
        )
    }
}
