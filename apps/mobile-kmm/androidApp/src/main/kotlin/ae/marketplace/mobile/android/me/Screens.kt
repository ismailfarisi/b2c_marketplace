package ae.marketplace.mobile.android.me

import ae.marketplace.mobile.android.NAV_BAR_HEIGHT
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.me.MeViewModel
import ae.marketplace.mobile.model.MyListing
import ae.marketplace.mobile.model.SavedItem
import ae.marketplace.mobile.model.SearchResult
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

internal val Accent = Color(0xFFB4652A)
internal val Ink = Color(0xFF0E0E10)
internal val Line = Color(0xFF2A2A2E)

/**
 * Clears the floating bottom bar, which is drawn *over* these lists rather than
 * beside them.
 *
 * Computed rather than a constant, because the bar itself adds
 * `navigationBarsPadding()` on top of its own height — so a fixed
 * `NAV_BAR_HEIGHT + margin` leaves the last row of every list under the gesture
 * bar on exactly the devices that have one. The first version of this hid the
 * Sign out button.
 */
@Composable
private fun bottomInset(): PaddingValues {
    val system = WindowInsets.navigationBars.asPaddingValues().calculateBottomPadding()
    return PaddingValues(bottom = NAV_BAR_HEIGHT + system + 20.dp)
}

// ─────────────────────────────────────────────────────────────────────────────
// Search
// ─────────────────────────────────────────────────────────────────────────────

/**
 * Search runs against the classifieds index, not the feed.
 *
 * Two surfaces, deliberately. The feed is discovery — geo-scoped, ranked by
 * watch behaviour, and it has no text box. Search is intent: someone who wants
 * a Patrol under 200k types that, and the structured facts to answer it live in
 * `vehicle_listings`. Conflating them would mean either a feed nobody can
 * filter by mileage or a search nobody can rank by attention.
 *
 * That is also why results are cards rather than reels: a person comparing
 * seven cars is not going to watch seven videos, and forcing them to is the
 * point at which they leave for a site that lists things.
 */
@Composable
fun SearchScreen(viewModel: MeViewModel, onOpen: (SearchResult) -> Unit, modifier: Modifier = Modifier) {
    val state by viewModel.search.collectAsState()

    Column(modifier.fillMaxSize().background(Ink).statusBarsPadding()) {
        Box(
            Modifier
                .padding(horizontal = 16.dp, vertical = 12.dp)
                .fillMaxWidth()
                .border(1.dp, Line, RoundedCornerShape(10.dp))
                .padding(horizontal = 14.dp, vertical = 13.dp),
        ) {
            if (state.query.isEmpty()) {
                Text(
                    stringResource(R.string.search_hint),
                    color = Color.White.copy(alpha = 0.3f),
                    fontSize = 15.sp,
                )
            }
            BasicTextField(
                value = state.query,
                onValueChange = viewModel::onQueryChanged,
                singleLine = true,
                textStyle = TextStyle(color = Color.White, fontSize = 15.sp),
                cursorBrush = SolidColor(Accent),
                keyboardOptions = KeyboardOptions(
                    keyboardType = KeyboardType.Text,
                    imeAction = ImeAction.Search,
                ),
                modifier = Modifier.fillMaxWidth(),
            )
        }

        when {
            state.loading && state.items.isEmpty() -> Centered { CircularProgressIndicator(color = Accent) }

            // Three empty states, not one. "Type something", "nothing matched"
            // and "that failed" are three different facts, and a single "No
            // results" would report a working search as broken to the person who
            // has not searched yet.
            !state.searched -> Hint(stringResource(R.string.search_prompt))
            state.error != null -> Hint(state.error ?: "", bad = true)
            state.items.isEmpty() -> Hint(stringResource(R.string.search_none, state.query))

            else -> LazyColumn(contentPadding = bottomInset()) {
                item {
                    Text(
                        text = stringResource(R.string.search_count, state.total),
                        color = Color.White.copy(alpha = 0.4f),
                        fontSize = 11.sp,
                        modifier = Modifier.padding(start = 18.dp, bottom = 6.dp),
                    )
                }
                items(state.items, key = { it.id }) { result ->
                    SearchRow(result) { onOpen(result) }
                }
            }
        }
    }
}

@Composable
private fun SearchRow(r: SearchResult, onClick: () -> Unit) {
    Column(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 13.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                // The server composes a title from year + make + model when the
                // seller gave none, so this fallback is for a row that somehow
                // has neither — never the common case.
                text = r.title.ifEmpty { r.referenceNo },
                color = Color.White,
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                modifier = Modifier.weight(1f),
            )
            if (r.promoted) {
                // Labelled, always. A promoted result that looks organic is what
                // makes buyers stop trusting the order — at which point the
                // placement the seller paid for is worth nothing.
                Text(
                    text = stringResource(R.string.search_promoted),
                    color = Accent,
                    fontSize = 9.sp,
                    fontWeight = FontWeight.Bold,
                    modifier = Modifier
                        .border(1.dp, Accent.copy(alpha = 0.6f), RoundedCornerShape(4.dp))
                        .padding(horizontal = 5.dp, vertical = 2.dp),
                )
            }
        }
        Text(
            text = listOfNotNull(
                if (r.mileageKm > 0) "${group(r.mileageKm.toLong())} km" else null,
                r.regionalSpec.uppercase().ifEmpty { null },
                r.city.ifEmpty { r.emirate }.ifEmpty { null },
                r.distanceM?.let { "${it / 1000} km away" },
            ).joinToString(" · "),
            color = Color.White.copy(alpha = 0.5f),
            fontSize = 12.sp,
            modifier = Modifier.padding(top = 3.dp),
        )
        Text(
            text = "${r.currency} ${group(r.priceMinor / 100)}",
            color = Accent,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.padding(top = 5.dp),
        )
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Saved
// ─────────────────────────────────────────────────────────────────────────────

/**
 * What the viewer kept.
 *
 * The words on each card are the vertical's, resolved through the same
 * SummarizePage the feed uses — so a saved car and a saved midnight offer come
 * through this one composable and it knows what neither of them is.
 */
@Composable
fun SavedScreen(viewModel: MeViewModel, onOpen: (SavedItem) -> Unit, modifier: Modifier = Modifier) {
    val state by viewModel.saved.collectAsState()
    LaunchedEffect(Unit) { viewModel.loadSaved() }

    Column(modifier.fillMaxSize().background(Ink).statusBarsPadding()) {
        ScreenTitle(stringResource(R.string.nav_saved))
        when {
            state.loading && state.items.isEmpty() -> Centered { CircularProgressIndicator(color = Accent) }
            state.error != null -> Hint(state.error ?: "", bad = true)
            state.items.isEmpty() -> Hint(stringResource(R.string.saved_empty))
            else -> LazyColumn(contentPadding = bottomInset()) {
                items(state.items, key = { it.reelId }) { item ->
                    SavedRow(item) { onOpen(item) }
                }
            }
        }
    }
}

@Composable
private fun SavedRow(item: SavedItem, onClick: () -> Unit) {
    val fade = if (item.unavailable) 0.4f else 1f
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(enabled = !item.unavailable, onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 10.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        // A flat block, not an image. There is no image loader in this app yet
        // — the feed paints video, not stills — so drawing a grey rectangle is
        // honest, whereas a broken-image glyph would read as a failure.
        Box(
            Modifier
                .size(width = 54.dp, height = 72.dp)
                .background(Color(0xFF1C1C20), RoundedCornerShape(6.dp))
        )
        Column(Modifier.weight(1f)) {
            val summary = item.summary
            Text(
                text = summary?.badge.orEmpty(),
                color = Color.White.copy(alpha = 0.45f * fade),
                fontSize = 9.sp,
                fontWeight = FontWeight.Bold,
            )
            Text(
                // No summary means the vertical has no provider, or its provider
                // failed. Either way the reel is still a real thing the viewer
                // kept, so the row renders rather than vanishing.
                text = summary?.title.orEmpty().ifEmpty { stringResource(R.string.saved_untitled) },
                color = Color.White.copy(alpha = fade),
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                modifier = Modifier.padding(top = 2.dp),
            )
            if (!summary?.line.isNullOrEmpty()) {
                Text(
                    text = summary.line,
                    color = Color.White.copy(alpha = 0.5f * fade),
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 2.dp),
                )
            }
            val price = summary?.priceMinor
            if (price != null && !summary.currency.isNullOrEmpty()) {
                Text(
                    text = "${summary.currency} ${group(price / 100)}${summary.priceSuffix}",
                    color = Accent.copy(alpha = fade),
                    fontSize = 14.sp,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
            if (item.unavailable) {
                Text(
                    text = stringResource(R.string.saved_gone),
                    color = Color.White.copy(alpha = 0.4f),
                    fontSize = 11.sp,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Me
// ─────────────────────────────────────────────────────────────────────────────

/**
 * The seller's own listings, in every state including the ugly ones.
 *
 * A rejected listing and a reel stuck in review are the two things a seller
 * actually comes here to find out about, and both are invisible everywhere else
 * in the product. Showing only live listings would leave the most anxious
 * person on the platform with a blank screen and no explanation.
 */
@Composable
fun MeScreen(
    viewModel: MeViewModel,
    onOpenInbox: () -> Unit,
    onSignOut: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.listings.collectAsState()
    LaunchedEffect(Unit) { viewModel.loadMyListings() }

    Column(modifier.fillMaxSize().background(Ink).statusBarsPadding()) {
        ScreenTitle(stringResource(R.string.nav_me))

        LazyColumn(contentPadding = bottomInset()) {
            item {
                RowLink(stringResource(R.string.me_messages), onOpenInbox)
                Text(
                    text = stringResource(R.string.me_your_listings),
                    color = Color.White.copy(alpha = 0.45f),
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier.padding(start = 18.dp, top = 20.dp, bottom = 4.dp),
                )
            }

            when {
                state.loading && state.items.isEmpty() ->
                    item { Centered(height = 120.dp) { CircularProgressIndicator(color = Accent) } }
                state.error != null -> item { Hint(state.error ?: "", bad = true) }
                state.items.isEmpty() -> item { Hint(stringResource(R.string.me_no_listings)) }
                else -> items(state.items, key = { it.id }) { ListingRow(it) }
            }

            item {
                Box(
                    Modifier
                        .padding(horizontal = 18.dp, vertical = 24.dp)
                        .fillMaxWidth()
                        .border(1.dp, Line, RoundedCornerShape(10.dp))
                        .clickable(onClick = onSignOut)
                        .padding(vertical = 13.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(stringResource(R.string.me_sign_out), color = Color.White.copy(alpha = 0.7f), fontSize = 14.sp)
                }
            }
        }
    }
}

@Composable
private fun ListingRow(l: MyListing) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 18.dp, vertical = 11.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = l.title.ifEmpty { l.referenceNo },
                color = Color.White,
                fontSize = 15.sp,
                fontWeight = FontWeight.Medium,
                modifier = Modifier.weight(1f),
            )
            StatusPill(l)
        }
        Text(
            text = listOfNotNull(
                if (l.mileageKm > 0) "${group(l.mileageKm.toLong())} km" else null,
                l.city.ifEmpty { null },
                "${l.currency} ${group(l.priceMinor / 100)}",
            ).joinToString(" · "),
            color = Color.White.copy(alpha = 0.5f),
            fontSize = 12.sp,
            modifier = Modifier.padding(top = 3.dp),
        )
        // The reel's state, when it is the thing holding the listing up. A
        // seller whose car is "pending review" while the video is still
        // transcoding is being told the wrong reason otherwise.
        reelNote(l)?.let {
            Text(it, color = Color.White.copy(alpha = 0.38f), fontSize = 11.sp, modifier = Modifier.padding(top = 3.dp))
        }
    }
}

@Composable
private fun StatusPill(l: MyListing) {
    val (label, colour) = when (l.status) {
        "active" -> stringResource(R.string.status_active) to Color(0xFF4CAF50)
        "pending_review" -> stringResource(R.string.status_review) to Accent
        "draft" -> stringResource(R.string.status_draft) to Color.White.copy(alpha = 0.4f)
        "rejected" -> stringResource(R.string.status_rejected) to Color(0xFFD9534F)
        "sold" -> stringResource(R.string.status_sold) to Color.White.copy(alpha = 0.4f)
        "paused" -> stringResource(R.string.status_paused) to Color.White.copy(alpha = 0.4f)
        "expired" -> stringResource(R.string.status_expired) to Color.White.copy(alpha = 0.4f)
        // A status this build predates still renders, as itself. Falling through
        // to nothing would make a new state invisible on the one screen that
        // exists to explain states.
        else -> l.status to Color.White.copy(alpha = 0.4f)
    }
    Text(
        text = label.uppercase(),
        color = colour,
        fontSize = 9.sp,
        fontWeight = FontWeight.Bold,
        modifier = Modifier
            .border(1.dp, colour.copy(alpha = 0.5f), RoundedCornerShape(4.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

@Composable
private fun reelNote(l: MyListing): String? = when {
    l.reelId == null -> null
    l.reelState == "published" -> null
    l.reelState == "review_pending" -> stringResource(R.string.reel_note_review)
    l.reelState == "rejected" -> stringResource(R.string.reel_note_rejected)
    l.reelState == "expired" -> stringResource(R.string.reel_note_expired)
    l.reelState == "failed" -> stringResource(R.string.reel_note_failed)
    l.reelState.isNotEmpty() -> stringResource(R.string.reel_note_processing)
    else -> null
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared
// ─────────────────────────────────────────────────────────────────────────────

@Composable
internal fun ScreenTitle(text: String) {
    Text(
        text = text,
        color = Color.White,
        fontSize = 22.sp,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = 18.dp, top = 12.dp, bottom = 10.dp),
    )
}

@Composable
private fun RowLink(label: String, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 15.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, color = Color.White, fontSize = 15.sp)
        Text("›", color = Color.White.copy(alpha = 0.4f), fontSize = 18.sp)
    }
}

@Composable
internal fun Centered(height: androidx.compose.ui.unit.Dp? = null, content: @Composable () -> Unit) {
    Box(
        Modifier.fillMaxWidth().then(if (height != null) Modifier.padding(vertical = 40.dp) else Modifier.fillMaxSize()),
        contentAlignment = Alignment.Center,
    ) { content() }
}

@Composable
internal fun Hint(text: String, bad: Boolean = false) {
    Box(Modifier.fillMaxWidth().padding(horizontal = 32.dp, vertical = 48.dp), Alignment.Center) {
        Text(
            text = text,
            color = if (bad) Accent else Color.White.copy(alpha = 0.45f),
            fontSize = 13.sp,
        )
    }
}

/** Thousands separator. Kotlin has no locale-aware formatter in common code. */
internal fun group(n: Long): String {
    val s = n.toString()
    if (s.length <= 3) return s
    return s.reversed().chunked(3).joinToString(",").reversed()
}

@Composable
internal fun BottomPad() {
    Box(Modifier.navigationBarsPadding().padding(bottom = NAV_BAR_HEIGHT))
}
