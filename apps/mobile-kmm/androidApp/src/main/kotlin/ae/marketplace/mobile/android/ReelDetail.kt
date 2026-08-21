package ae.marketplace.mobile.android

import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.model.ResolvedAction
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp

/**
 * One reel, opened because someone asked for it.
 *
 * # What this replaced
 *
 * Tapping a row in Saved navigated to the feed *in general* — wherever it
 * happened to be, showing whatever the ranker had chosen next. The viewer had
 * bookmarked one specific car and got a different one, which is the least
 * useful possible response to a bookmark and made the whole Saved tab decorative.
 *
 * # Why it is drawn over the feed rather than replacing it
 *
 * Two reasons, and the second is the load-bearing one.
 *
 * The feed's pager keeps its position, its loaded pages and its decoder
 * attachments while this is on screen, so dismissing returns the viewer to
 * exactly where they were rather than to the top of a freshly fetched page.
 *
 * And there is only one [ae.marketplace.mobile.playback.PlaybackCoordinator].
 * A separate screen with its own would mean a second pool of hardware decoders
 * held open behind the first, which on a mid-range Android device is how the
 * next video anywhere in the app fails to start — with no error that names the
 * cause. This borrows the feed's, one item long.
 */
@Composable
fun ReelDetail(
    viewModel: FeedViewModel,
    returnToIndex: Int,
    onActionPressed: (ResolvedAction, String) -> Unit,
    onClose: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val focused by viewModel.focused.collectAsState()
    val feed by viewModel.state.collectAsState()

    if (!focused.open) return

    val dismiss = {
        viewModel.closeReel(returnToIndex)
        onClose()
    }
    BackHandler(onBack = dismiss)

    val closeLabel = stringResource(R.string.action_close)

    Box(modifier.fillMaxSize().background(Color.Black)) {
        val item = focused.item
        when {
            focused.loading || (item == null && focused.error == null) -> LoadingBlock()

            focused.error != null -> Box(Modifier.fillMaxSize(), Alignment.Center) {
                // A saved reel that 404s is the common case here — taken down,
                // sold, expired — and NotFound is deliberately not retryable, so
                // this renders without a button that could not help.
                FailureBlock(focused.error!!, onRetry = viewModel::retryReel)
            }

            item != null -> {
                ReelSurface(
                    reelId = item.id,
                    isActive = true,
                    viewModel = viewModel,
                    posterUrl = item.posterUrl,
                    lqip = item.lqip,
                    modifier = Modifier.fillMaxSize(),
                )

                // The same gradient the feed uses, for the same reason: the
                // background is an arbitrary photograph and white text over a
                // bright one is not there.
                Box(
                    Modifier
                        .fillMaxSize()
                        .background(
                            Brush.verticalGradient(
                                0.0f to Color.Black.copy(alpha = 0.55f),
                                0.18f to Color.Transparent,
                                0.55f to Color.Transparent,
                                1.0f to Color.Black.copy(alpha = 0.75f),
                            )
                        )
                )

                ReelOverlay(
                    item = item,
                    skewMillis = feed.clockSkewMillis,
                    onAction = { action ->
                        // Recorded first, exactly as the feed does — including
                        // when the target turns out to be unavailable. A client
                        // that only reports successful navigations flatters the
                        // conversion rate by the number of dead CTAs.
                        viewModel.onActionClick(action.id, item.id)
                        onActionPressed(action, item.id)
                    },
                    modifier = Modifier.fillMaxSize(),
                    // No tab row above and no nav bar below — this screen is
                    // modal, so the overlay gets the whole height. Passing the
                    // feed's chrome insets here would leave two unexplained
                    // bands of empty space.
                    chromeTop = 0.dp,
                    chromeBottom = 0.dp,
                    onLike = { viewModel.toggleLike(item.id) },
                    onSave = { viewModel.toggleSave(item.id) },
                )
            }
        }

        // Drawn last so it is above the overlay and always reachable, including
        // over a CTA. The system back gesture does the same thing, but a modal
        // that can only be left by a gesture is a modal a fair number of people
        // are stuck in.
        //
        // Top-*right*, not top-left, which is where it started and where it sat
        // directly on top of the badge — "✕SED CAR". The overlay owns the left
        // edge for the subject's own label on every vertical, so anything this
        // screen adds has to go on the other side.
        Box(
            contentAlignment = Alignment.Center,
            modifier = Modifier
                .align(Alignment.TopEnd)
                .statusBarsPadding()
                .padding(end = Space.Large, top = Space.Small)
                .size(36.dp)
                .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(50))
                .clickable(onClick = dismiss)
                .semantics { contentDescription = closeLabel },
        ) {
            Icon(
                imageVector = Icons.Filled.Close,
                contentDescription = null,
                tint = Palette.Text,
                modifier = Modifier.size(20.dp),
            )
        }
    }
}
