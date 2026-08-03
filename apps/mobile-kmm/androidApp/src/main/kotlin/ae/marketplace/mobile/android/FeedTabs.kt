package ae.marketplace.mobile.android

import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.words
import ae.marketplace.mobile.feed.FeedEmptyReason
import ae.marketplace.mobile.feed.FeedSurface
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.isRetryable
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// Was a private copy of the same hex in four files, two of which had drifted.
// See ae.marketplace.mobile.android.ui.Palette.
private val Accent = Palette.Accent

/**
 * The four tabs.
 *
 * Horizontally scrollable rather than four equal weights. Four short English
 * words fit; "Ending soon" in Arabic does not always, and a tab row that
 * truncates its own labels is worse than one that scrolls — the alternative is
 * choosing shorter words for the language rather than the meaning.
 *
 * Highlighting follows [selected], which is what the viewer tapped, not what the
 * server served. The two differ only when this build asks for a tab the server
 * has retired, and in that moment following the request keeps the row stable
 * under the thumb while the empty state explains itself.
 */
@Composable
fun FeedTabs(
    selected: FeedSurface,
    onSelect: (FeedSurface) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = 12.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        FeedSurface.entries.forEach { surface ->
            Tab(
                label = stringResource(surface.labelRes()),
                active = surface == selected,
                onClick = { onSelect(surface) },
            )
        }
    }
}

@Composable
private fun Tab(label: String, active: Boolean, onClick: () -> Unit) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier
            .clickable(onClick = onClick)
            .padding(horizontal = 10.dp, vertical = 10.dp),
    ) {
        Text(
            text = label,
            color = if (active) Color.White else Color.White.copy(alpha = 0.65f),
            fontSize = 14.sp,
            fontWeight = if (active) FontWeight.SemiBold else FontWeight.Normal,
        )
        // A 2dp rule under the active tab rather than a filled chip. The tab row
        // floats over video, and a filled background would need a scrim heavy
        // enough to compete with the reel underneath it.
        Box(
            Modifier
                .padding(top = 4.dp)
                .height(2.dp)
                .width(if (active) 20.dp else 0.dp)
                .background(if (active) Accent else Color.Transparent),
        )
    }
}

private fun FeedSurface.labelRes(): Int = when (this) {
    FeedSurface.ForYou -> R.string.surface_for_you
    FeedSurface.LiveNow -> R.string.surface_live_now
    FeedSurface.EndingSoon -> R.string.surface_ending_soon
    FeedSurface.Nearby -> R.string.surface_nearby
}

/**
 * What sits where the reels would be: loading, a failure, or an empty surface.
 *
 * All three used to render the same way — as the empty state — because the only
 * question asked was whether the list was empty. It is empty in all three cases
 * and they mean completely different things:
 *
 *  - **Loading.** The list is empty for the whole of the first fetch, so every
 *    cold start opened on "Nothing here for you" and then replaced it with
 *    reels. Told, on launch, that the marketplace is empty.
 *  - **Failed.** Now says which failure and offers a retry. Before, the only
 *    way to retry a failed feed was to switch tabs and switch back — and the
 *    generic line gave the viewer no reason to think that would help.
 *  - **Genuinely empty.** Deliberately not the same sentence four times.
 *    "Nothing is live right now" is a true statement about a working product;
 *    the generic line describes a broken one, and which the viewer believes
 *    decides whether they try another tab or close the app. The reason comes
 *    from the server because the client cannot derive it — "no reels" and "no
 *    reels that are live" are the same empty array.
 */
@Composable
fun FeedEmptyState(
    reason: FeedEmptyReason?,
    error: Failure?,
    loading: Boolean,
    onRetry: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(modifier.fillMaxWidth().padding(32.dp), Alignment.Center) {
        when {
            // Ordered before the failure on purpose: a retry sets both, and
            // leaving the previous error on screen under a spinner tells the
            // viewer the retry has already failed.
            loading -> CircularProgressIndicator(
                color = Color.White.copy(alpha = 0.7f),
                strokeWidth = 2.dp,
                modifier = Modifier.size(28.dp),
            )

            // The app's own failure block, not a hand-rolled one. The first
            // version of this built its own Text + TextButton and the retry
            // came out Material3 default purple on a black feed, because a
            // bare TextButton inherits the theme's primary colour and this app
            // never set one. Reusing the component gets the palette, the
            // isRetryable rule and the button style that everything else uses.
            error != null -> FailureBlock(failure = error, onRetry = onRetry)

            else -> Text(
                when (reason) {
                    FeedEmptyReason.NothingLive -> stringResource(R.string.empty_live)
                    FeedEmptyReason.NothingClosing -> stringResource(R.string.empty_closing)
                    FeedEmptyReason.NothingNear -> stringResource(R.string.empty_near)
                    else -> stringResource(R.string.empty_eligible)
                },
                color = Color.White.copy(alpha = 0.7f),
                fontSize = 14.sp,
                textAlign = TextAlign.Center,
            )
        }
    }
}
