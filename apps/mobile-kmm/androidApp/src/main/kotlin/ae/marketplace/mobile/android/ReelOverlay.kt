package ae.marketplace.mobile.android

import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.feed.ReelClock
import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.model.ResolvedAction
import ae.marketplace.mobile.model.SubjectSummary
import ae.marketplace.mobile.model.TimeSignal
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay

/**
 * Everything drawn over the video.
 *
 * The overlay is the same skeleton for every vertical — bar, badge, time pill,
 * two lines, a price, one action — and only the *time signal* changes between
 * them. That is the design holding: there is no `when (subjectType)` in this
 * file, because the moment one exists, adding a vertical means shipping a
 * release.
 *
 * All the arithmetic is in `:shared`. What is here is a rectangle, some text and
 * a timer, which is the part that genuinely has to be written twice.
 */

// Was a private copy of the same hex in four files, two of which had drifted.
// See ae.marketplace.mobile.android.ui.Palette.
private val Accent = Palette.Accent
private val Scrim = Color.Black.copy(alpha = 0.55f)

/**
 * How often the clock is re-read.
 *
 * One second, and only while a countdown is on screen. A frame-rate ticker on
 * something that renders whole seconds burns battery to redraw identical text —
 * and it is the reel *player* that needs the frames.
 */
private const val TICK_MS = 1_000L

@Composable
fun ReelOverlay(
    item: FeedItem,
    skewMillis: Long,
    onAction: (ResolvedAction) -> Unit,
    modifier: Modifier = Modifier,
    /**
     * Vertical space reserved for the tab row, which is drawn by the feed above
     * this overlay and in the same Box.
     *
     * Passed in rather than hard-coded because the two are laid out
     * independently: the tabs live once at the feed level and the overlay once
     * per page, so nothing makes them agree except this number. The first build
     * had them both at the top and the badge rendered straight through
     * "Nearby".
     */
    chromeTop: Dp = 0.dp,
    /**
     * Space reserved at the bottom for the navigation bar, which is drawn by the
     * activity over this overlay. Same reasoning as [chromeTop]: the two are laid
     * out independently and nothing else makes them agree — without it the price
     * and the CTA sit underneath "Sell".
     */
    chromeBottom: Dp = 0.dp,
) {
    Box(modifier.fillMaxWidth()) {
        Column(Modifier.align(Alignment.TopStart).fillMaxWidth()) {
            // The bar stays above everything, including the tabs — it is the
            // page's own progress and belongs at the very edge of the screen.
            TimeBar(item.timeSignal, skewMillis)
            Row(
                Modifier
                    .fillMaxWidth()
                    .statusBarsPadding()
                    .padding(top = chromeTop)
                    .padding(16.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                item.summary?.badge?.takeIf { it.isNotBlank() }?.let { Pill(it, filled = true) }
                TimePill(item.timeSignal, skewMillis)
            }
        }

        Column(
            Modifier
                .align(Alignment.BottomStart)
                .fillMaxWidth()
                .navigationBarsPadding()
                .padding(bottom = chromeBottom)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            ProductLine(item)
            ActionRow(item.actions, onAction)
        }
    }
}

/**
 * The thin bar at the very top.
 *
 * `fraction` arrives already oriented by ReelClock — a countdown falls, a window
 * rises — so there is nothing here that knows which way round a mode goes. That
 * question was the most likely place for the two platforms to disagree, which is
 * exactly why it is answered once, in shared code, under test.
 */
@Composable
private fun TimeBar(signal: TimeSignal?, skewMillis: Long) {
    val face = signal?.let { tickingFace(it, skewMillis) } ?: return

    Box(Modifier.fillMaxWidth().height(3.dp).background(Color.White.copy(alpha = 0.22f))) {
        Box(
            Modifier
                .fillMaxWidth(face.fraction)
                .height(3.dp)
                .background(Accent),
        )
    }
}

@Composable
private fun TimePill(signal: TimeSignal?, skewMillis: Long) {
    val face = signal?.let { tickingFace(it, skewMillis) } ?: return
    Pill(text = labelText(face.label), filled = false)
}

/**
 * Re-resolves the signal on a timer, and only when there is something to
 * animate.
 *
 * A freshness signal changes once a day; ticking it every second would keep the
 * whole feed awake to redraw "listed 3d ago". So the ticker runs for the two
 * modes whose text actually moves, and the other two resolve once per
 * composition.
 */
@Composable
private fun tickingFace(signal: TimeSignal, skewMillis: Long): ReelClock.Face? {
    val ticking = signal.mode == ReelClock.MODE_COUNTDOWN || signal.mode == ReelClock.MODE_WINDOW
    var now by remember(signal) { mutableLongStateOf(System.currentTimeMillis()) }

    LaunchedEffect(signal, ticking) {
        if (!ticking) return@LaunchedEffect
        while (true) {
            delay(TICK_MS)
            now = System.currentTimeMillis()
        }
    }
    return ReelClock.face(signal, now, skewMillis)
}

/**
 * Renders a resolved label.
 *
 * The switch is over [ReelClock.Label], which is a closed set of shapes, and not
 * over the mode string — so a mode this build has never heard of arrives as a
 * null face and draws nothing rather than falling into an `else` that guesses.
 */
@Composable
private fun labelText(label: ReelClock.Label): String = when (label) {
    is ReelClock.Label.Remaining ->
        if (label.hours > 0) {
            stringResource(R.string.time_remaining_h, label.hours, label.minutes)
        } else {
            stringResource(R.string.time_remaining_ms, label.minutes, label.seconds)
        }

    is ReelClock.Label.ElapsedOf -> stringResource(
        R.string.time_window,
        label.remainingMinutes / 60,
        label.remainingMinutes % 60,
        label.totalMinutes / 60,
    )

    is ReelClock.Label.Opens ->
        if (label.hours > 0) {
            stringResource(R.string.time_opens_h, label.hours, label.minutes)
        } else {
            stringResource(R.string.time_opens_m, label.minutes)
        }

    is ReelClock.Label.Ago -> {
        val unit = when (label.unit) {
            ReelClock.Label.Unit.MINUTE -> R.plurals.ago_minutes
            ReelClock.Label.Unit.HOUR -> R.plurals.ago_hours
            ReelClock.Label.Unit.DAY -> R.plurals.ago_days
            ReelClock.Label.Unit.WEEK -> R.plurals.ago_weeks
        }
        androidx.compose.ui.res.pluralStringResource(unit, label.amount, label.amount)
    }

    is ReelClock.Label.By ->
        stringResource(R.string.time_order_by, label.hour, label.minute)

    ReelClock.Label.Closed -> stringResource(R.string.time_closed)
}

@Composable
private fun Pill(text: String, filled: Boolean) {
    val shape = RoundedCornerShape(50)
    Box(
        Modifier
            .then(if (filled) Modifier.background(Scrim, shape) else Modifier)
            .then(if (filled) Modifier else Modifier.border(1.dp, Color.White.copy(alpha = 0.5f), shape))
            .padding(horizontal = 12.dp, vertical = 6.dp),
    ) {
        Text(
            text = text,
            color = Color.White,
            fontSize = 11.sp,
            fontWeight = FontWeight.Medium,
            // dir is inherited from the layout, but the *content* may be in
            // either language regardless of the app's locale — a vendor writes
            // their badge in whichever they chose.
            maxLines = 1,
        )
    }
}

/**
 * Title, qualifier line, price.
 *
 * Falls back to caption and author when a subject has no registered provider,
 * which is the majority of reels today. That path is not a degraded state — it
 * is what Phase 2 shipped and it still reads fine — so it gets the same layout
 * rather than an empty gap where a title would be.
 */
@Composable
private fun ProductLine(item: FeedItem) {
    val summary: SubjectSummary? = item.summary

    Text(
        text = summary?.title?.takeIf { it.isNotBlank() }
            ?: item.author.name.ifBlank { "—" },
        color = Color.White,
        fontWeight = FontWeight.SemiBold,
        fontSize = 16.sp,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
    )

    val line = summary?.line?.takeIf { it.isNotBlank() } ?: item.caption
    if (line.isNotBlank()) {
        Text(
            text = line,
            color = Color.White.copy(alpha = 0.8f),
            fontSize = 13.sp,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }

    // A currency of null means this subject has no price to show, which is not
    // the same as free — rendering it as 0 would put every car in the feed at
    // AED 0.00.
    summary?.currency?.let { currency ->
        Text(
            text = formatPrice(summary.priceMinor ?: 0L, currency) + summary.priceSuffix,
            color = Accent,
            fontWeight = FontWeight.SemiBold,
            fontSize = 18.sp,
        )
    }
}

/**
 * Minor units to a displayable amount.
 *
 * Two decimal places dropped when they are zero, because every price in this
 * catalogue is whole dirhams and "AED 189,000.00" is four characters of noise on
 * a line that has to be read in about a second.
 */
private fun formatPrice(minor: Long, currency: String): String {
    val major = minor / 100
    val cents = minor % 100
    val grouped = major.toString().reversed().chunked(3).joinToString(",").reversed()
    return if (cents == 0L) "$currency $grouped"
    else "$currency $grouped.${cents.toString().padStart(2, '0')}"
}

@Composable
private fun ActionRow(actions: List<ResolvedAction>, onAction: (ResolvedAction) -> Unit) {
    if (actions.isEmpty()) return

    Row(
        Modifier.fillMaxWidth().padding(top = 6.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        actions.sortedBy { it.position }.forEach { action ->
            Column(Modifier.weight(1f)) {
                Button(
                    onClick = { onAction(action) },
                    enabled = action.available,
                    shape = RoundedCornerShape(6.dp),
                    modifier = Modifier.fillMaxWidth(),
                    colors = ButtonDefaults.buttonColors(
                        containerColor = Accent,
                        contentColor = Color.White,
                    ),
                ) {
                    // Rendered exactly as the vertical's handler gave it. There
                    // is deliberately no `when (action.targetType)` here — the
                    // moment one exists, adding a vertical means shipping a
                    // mobile release.
                    Text(action.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                // Always shown when a CTA is unavailable. A greyed-out button
                // with no explanation reads as a broken app, and the vendor gets
                // blamed for a platform decision.
                action.reason?.takeIf { !action.available }?.let {
                    Text(it, color = Color.White.copy(alpha = 0.6f), fontSize = 11.sp)
                }
            }
        }
    }
}
