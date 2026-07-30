package ae.marketplace.mobile.android

import ae.marketplace.mobile.feed.FeedEmptyReason
import ae.marketplace.mobile.feed.FeedSurface
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

private val Accent = Color(0xFFB4652A)

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
 * The empty state, per surface.
 *
 * Deliberately not the same sentence four times. "Nothing is live right now" is
 * a true statement about a working product; the generic line describes a broken
 * one, and which the viewer believes decides whether they try another tab or
 * close the app. The reason comes from the server because the client cannot
 * derive it — "no reels" and "no reels that are live" are the same empty array.
 */
@Composable
fun FeedEmptyState(reason: FeedEmptyReason?, error: String?, modifier: Modifier = Modifier) {
    val text = when {
        error != null -> stringResource(R.string.feed_failed)
        reason == FeedEmptyReason.NothingLive -> stringResource(R.string.empty_live)
        reason == FeedEmptyReason.NothingClosing -> stringResource(R.string.empty_closing)
        reason == FeedEmptyReason.NothingNear -> stringResource(R.string.empty_near)
        else -> stringResource(R.string.empty_eligible)
    }

    Box(modifier.fillMaxWidth().padding(32.dp), Alignment.Center) {
        Text(text, color = Color.White.copy(alpha = 0.7f), fontSize = 14.sp)
    }
}
