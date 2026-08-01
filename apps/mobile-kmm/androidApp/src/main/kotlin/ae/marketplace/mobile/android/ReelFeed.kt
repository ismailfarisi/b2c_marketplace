package ae.marketplace.mobile.android

import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.model.ResolvedAction
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.pager.VerticalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp

/**
 * The feed.
 *
 * Small on purpose. Every decision that costs money or corrupts data — which
 * rendition, how far to prefetch, what counts as a view, when to page, and now
 * how a countdown resolves against a device clock that is wrong — lives in
 * `:shared` and is tested there. What is left here is a pager, a surface and
 * some text, which is the part that genuinely has to be written twice because
 * Compose and SwiftUI have nothing in common.
 */
@Composable
fun ReelFeed(
    viewModel: FeedViewModel,
    onOpenFilters: () -> Unit = {},
    /**
     * Where a CTA press goes once it has been recorded.
     *
     * Hoisted out of this composable because the destination is a different
     * screen and the feed must not know how to build one. The host decides —
     * see MainActivity, where the branch is on the action's *kind* and never on
     * its target type, which is the line this whole seam is drawn along.
     */
    onActionPressed: (ResolvedAction, String) -> Unit = { _, _ -> },
) {
    val state by viewModel.state.collectAsState()

    val pagerState = rememberPagerState { state.items.size }

    // Back to the top on a tab change. The pager otherwise keeps its scroll
    // offset across surfaces, so switching to a two-item Live now feed lands the
    // viewer on page seven and the screen looks blank.
    //
    // scrollToPage, not animateScrollToPage: there is nothing to animate between
    // — the list underneath has been replaced — and animating would flick
    // through reels from a feed the viewer has already left, each of which would
    // report an impression on the way past.
    LaunchedEffect(state.surface) { pagerState.scrollToPage(0) }

    // `settledPage`, not `currentPage`. currentPage changes the moment a drag
    // crosses the midpoint, so reporting on it counts a view for every reel a
    // viewer half-swipes and pulls back from — which inflates impressions on
    // exactly the content people rejected.
    LaunchedEffect(pagerState, state.items.size) {
        snapshotFlow { pagerState.settledPage }
            .collect { page -> if (state.items.isNotEmpty()) viewModel.onVisible(page) }
    }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        if (state.items.isEmpty()) {
            // The empty state sits under the tab row rather than replacing it,
            // and that is the point of naming *why* a surface is empty: the
            // message exists to send the viewer to another tab, and a screen
            // with no tabs on it gives them nowhere to go.
            Box(Modifier.fillMaxSize(), Alignment.Center) {
                FeedEmptyState(
                    reason = state.emptyReason,
                    error = state.error,
                    loading = state.loading,
                    onRetry = viewModel::refresh,
                )
            }
        } else {
            VerticalPager(
                state = pagerState,
                modifier = Modifier.fillMaxSize(),
                // One page held either side. The decoder pool in :shared is
                // sized for more than this; the composition window is smaller
                // because keeping three surfaces attached costs GPU memory that
                // the decoders do not.
                beyondViewportPageCount = 1,
            ) { page ->
                ReelPage(
                    item = state.items[page],
                    isActive = page == pagerState.settledPage,
                    skewMillis = state.clockSkewMillis,
                    viewModel = viewModel,
                    onLike = { viewModel.toggleLike(state.items[page].id) },
                    onAction = { action ->
                        // Recorded first, always — including when the target
                        // turns out to be unavailable. Phase 3 built the funnel
                        // with action_click as conversion's denominator, and a
                        // client that only reports successful navigations
                        // flatters the rate by exactly the number of dead CTAs.
                        viewModel.onActionClick(action.id, state.items[page].id)
                        onActionPressed(action, state.items[page].id)
                    },
                )
            }
        }

        // Drawn last so it sits above the pager, and inside the same Box rather
        // than in a Scaffold slot because the reel is full-bleed by design — the
        // video runs behind the tabs and under the status bar, and a top app bar
        // would cut a letterbox out of it.
        Column(Modifier.fillMaxWidth().statusBarsPadding()) {
            FeedTabs(
                selected = state.surface,
                onSelect = viewModel::onSurfaceSelected,
                modifier = Modifier.padding(top = 4.dp),
            )
            // Its own line, under the surfaces rather than beside them. Sharing
            // the row cost "Nearby" its last three characters: four tab labels
            // and a filter summary do not fit across a phone, and the tab row
            // scrolls, so the casualty was silent.
            //
            // Under rather than over, because the two controls are not
            // alternatives — a surface reorders the eligible set, a filter
            // changes what is eligible — and stacking them says that more
            // clearly than one row of mixed chips would.
            FilterButton(
                state = state,
                onClick = onOpenFilters,
                modifier = Modifier.padding(start = 22.dp, top = 2.dp),
            )
        }
    }
}

/**
 * The height of the whole top chrome — surface tabs plus the filter summary
 * under them — shared by the rows themselves and by the overlay that has to
 * clear them.
 *
 * A constant rather than a measurement because both rows are fixed stacks of
 * text, and an onGloballyPositioned for one number would add a layout pass to
 * every frame of a video feed.
 */
private val TAB_ROW_HEIGHT = 86.dp

@Composable
private fun ReelPage(
    item: FeedItem,
    isActive: Boolean,
    skewMillis: Long,
    viewModel: FeedViewModel,
    onLike: () -> Unit,
    onAction: (ResolvedAction) -> Unit,
) {
    Box(Modifier.fillMaxSize().background(Color.Black)) {

        ReelSurface(
            reelId = item.id,
            isActive = isActive,
            viewModel = viewModel,
            // Drawn under the player until it reports a rendered frame. Without
            // it a swipe reveals black for as long as a manifest fetch and a
            // decode take, on the one screen whose whole promise is that the
            // next thing is already there.
            posterUrl = item.posterUrl,
            lqip = item.lqip,
            modifier = Modifier.fillMaxSize(),
        )

        // Gradients top and bottom, under the overlay and over the video.
        //
        // This mattered less while every reel was a black rectangle. Now that
        // posters render — and once real footage does — the background under
        // the title, the price and the CTA is an arbitrary photograph, and
        // white text on a white car in Dubai sunlight is simply not there.
        //
        // A gradient rather than the flat per-pill scrims already in the
        // overlay, because those solve a different problem: a pill is a small
        // opaque shape and a hard edge is fine on it, whereas a hard edge
        // across the whole lower third reads as a black bar someone forgot to
        // remove. The stops are weighted towards the bottom because that is
        // where four lines of text sit; the top only has to carry a badge.
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

        // Time bar, badge, time pill, product line, CTAs. One skeleton for every
        // vertical; only the time signal differs between them.
        ReelOverlay(
            item = item,
            skewMillis = skewMillis,
            onAction = onAction,
            modifier = Modifier.fillMaxSize(),
            // Height of the tab row below. Kept next to the FeedTabs call that
            // creates it so the two move together.
            chromeTop = TAB_ROW_HEIGHT,
            chromeBottom = NAV_BAR_HEIGHT,
        )

        Text(
            text = if (item.viewer.liked) "♥ ${item.counters.likes}" else "♡ ${item.counters.likes}",
            color = Color.White,
            modifier = Modifier
                .align(Alignment.CenterEnd)
                .padding(16.dp)
                .clickable(onClick = onLike),
        )
    }
}
