package ae.marketplace.mobile.android

import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.playback.asExoPlayer
import androidx.compose.foundation.background
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.viewinterop.AndroidView
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView

/**
 * Attaches the shared player to a surface.
 *
 * The whole of the Android video integration, and it is short because the parts
 * that are hard — which rendition, how many players, when to prepare — are in
 * `:shared` and already decided by the time this runs. All that is left is
 * handing a `SurfaceView` to a decoder somebody else created.
 *
 * `useController = false` because a feed does not have a scrub bar. The default
 * controller intercepts touches, which breaks the vertical pager's drag in a way
 * that looks like a Compose bug for about a day.
 */
@androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
@Composable
fun ReelSurface(
    reelId: String,
    isActive: Boolean,
    viewModel: FeedViewModel,
    modifier: Modifier = Modifier,
) {
    val player = viewModel.playerFor(reelId)?.asExoPlayer()

    if (player == null) {
        // Outside the prefetch window. Drawing black rather than creating a
        // decoder here is the point of the null: a surface that eagerly made its
        // own player would defeat the pool and the egress policy in one line.
        androidx.compose.foundation.layout.Box(modifier.background(Color.Black))
        return
    }

    AndroidView(
        modifier = modifier,
        factory = { context ->
            PlayerView(context).apply {
                useController = false
                // Fill and crop. A vertical reel on a tall phone letterboxed
                // into a 16:9 box is the single most obvious sign of a feed
                // built by someone who did not watch it.
                resizeMode = AspectRatioFrameLayout.RESIZE_MODE_ZOOM
                setShutterBackgroundColor(android.graphics.Color.BLACK)
            }
        },
        update = { view -> view.player = player },
        onRelease = { view ->
            // Detach, never release. The player belongs to the shared pool and
            // is about to be reused for another reel; releasing it here would
            // destroy a decoder the coordinator still thinks it owns.
            view.player = null
        },
    )

    DisposableEffect(reelId, isActive) {
        onDispose { }
    }
}
