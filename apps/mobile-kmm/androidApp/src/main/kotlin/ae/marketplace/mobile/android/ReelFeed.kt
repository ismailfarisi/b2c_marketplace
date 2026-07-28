package ae.marketplace.mobile.android

import ae.marketplace.mobile.feed.FeedViewModel
import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.model.ResolvedAction
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.pager.VerticalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The feed.
 *
 * Small on purpose. Every decision that costs money or corrupts data —
 * which rendition, how far to prefetch, what counts as a view, when to page —
 * lives in `:shared` and is tested there. What is left here is a pager, a
 * surface and some text, which is the part that genuinely has to be written
 * twice because Compose and SwiftUI have nothing in common.
 */
@Composable
fun ReelFeed(viewModel: FeedViewModel) {
    val state by viewModel.state.collectAsState()
    val pagerState = rememberPagerState(pageCount = { state.items.size })

    // `settledPage`, not `currentPage`. currentPage changes the moment a drag
    // crosses the midpoint, so reporting on it counts a view for every reel a
    // viewer half-swipes and pulls back from — which inflates impressions on
    // exactly the content people rejected.
    LaunchedEffect(pagerState, state.items.size) {
        snapshotFlow { pagerState.settledPage }
            .collect { page -> if (state.items.isNotEmpty()) viewModel.onVisible(page) }
    }

    if (state.items.isEmpty()) {
        Box(Modifier.fillMaxSize().background(Color.Black), Alignment.Center) {
            Text(
                text = state.error ?: "Nothing here yet",
                color = Color.White.copy(alpha = 0.7f),
            )
        }
        return
    }

    VerticalPager(
        state = pagerState,
        modifier = Modifier.fillMaxSize(),
        // One page held either side. The decoder pool in :shared is sized for
        // more than this; the composition window is smaller because keeping
        // three surfaces attached costs GPU memory that the decoders do not.
        beyondViewportPageCount = 1,
    ) { page ->
        ReelPage(
            item = state.items[page],
            isActive = page == pagerState.settledPage,
            viewModel = viewModel,
            onLike = { viewModel.toggleLike(state.items[page].id) },
            onAction = { action -> viewModel.onActionClick(action.id, state.items[page].id) },
        )
    }
}

@Composable
private fun ReelPage(
    item: FeedItem,
    isActive: Boolean,
    viewModel: FeedViewModel,
    onLike: () -> Unit,
    onAction: (ResolvedAction) -> Unit,
) {
    Box(Modifier.fillMaxSize().background(Color.Black)) {

        ReelSurface(
            reelId = item.id,
            isActive = isActive,
            viewModel = viewModel,
            modifier = Modifier.fillMaxSize(),
        )

        Column(
            modifier = Modifier
                .align(Alignment.BottomStart)
                .fillMaxWidth()
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(
                text = item.author.name.ifBlank { "—" },
                color = Color.White,
                fontWeight = FontWeight.SemiBold,
            )
            if (item.caption.isNotBlank()) {
                Text(text = item.caption, color = Color.White, fontSize = 14.sp, maxLines = 3)
            }

            item.actions.sortedBy { it.position }.forEach { action ->
                Button(
                    onClick = { onAction(action) },
                    enabled = action.available,
                    colors = ButtonDefaults.buttonColors(),
                ) {
                    // The label comes from the vertical's handler and is
                    // rendered as given. There is deliberately no
                    // `when (action.targetType)` anywhere in this app — the
                    // moment one exists, adding a vertical means shipping a
                    // mobile release.
                    Text(action.label)
                }
                // Always shown when a CTA is unavailable. A greyed-out button
                // with no explanation reads as a broken app, and the vendor
                // gets blamed for a platform decision.
                if (!action.available && action.reason != null) {
                    Text(action.reason!!, color = Color.White.copy(alpha = 0.6f), fontSize = 12.sp)
                }
            }
        }

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
