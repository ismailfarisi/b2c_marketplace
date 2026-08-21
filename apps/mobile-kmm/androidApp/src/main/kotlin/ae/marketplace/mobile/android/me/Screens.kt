package ae.marketplace.mobile.android.me

import ae.marketplace.mobile.android.NAV_BAR_HEIGHT
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.EmptyBlock
import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.FailureStrip
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Poster
import ae.marketplace.mobile.android.ui.Shape
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
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
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

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
    return PaddingValues(bottom = NAV_BAR_HEIGHT + system + Space.XL)
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
fun SearchScreen(
    viewModel: MeViewModel,
    onOpen: (SearchResult) -> Unit,
    hasLocation: Boolean,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.search.collectAsState()
    val keyboard = LocalSoftwareKeyboardController.current
    val listState = rememberLazyListState()

    // Fetch the next page a few rows before the bottom rather than at it.
    //
    // Triggering on the last visible item means the viewer reaches the end of
    // the list and waits; four rows of lead time is roughly one flick, which is
    // usually enough for the request to land before they get there.
    val nearEnd by remember {
        derivedStateOf {
            val last = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
            last >= listState.layoutInfo.totalItemsCount - 4
        }
    }
    LaunchedEffect(nearEnd, state.hasMore) {
        if (nearEnd && state.hasMore) viewModel.loadMore()
    }

    state.draftFilters?.let { draft ->
        SearchFilterSheet(
            viewModel = viewModel,
            draft = draft,
            hasLocation = hasLocation,
            onDismiss = viewModel::cancelFilters,
        )
    }

    Column(modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding()) {
        Row(
            Modifier
                .padding(horizontal = Space.Large, vertical = Space.Medium)
                .fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(Space.Small),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .weight(1f)
                    .background(Palette.Surface, RoundedCornerShape(50))
                    .padding(horizontal = 14.dp, vertical = 13.dp),
            ) {
                Icon(
                    imageVector = Icons.Outlined.Search,
                    contentDescription = null,
                    tint = Palette.TextFaint,
                    modifier = Modifier.size(18.dp),
                )
                Box(Modifier.weight(1f).padding(start = 10.dp)) {
                    if (state.query.isEmpty()) {
                        Text(
                            stringResource(R.string.search_hint),
                            color = Palette.TextGhost,
                            fontSize = Type.Body,
                        )
                    }
                    BasicTextField(
                        value = state.query,
                        onValueChange = viewModel::onQueryChanged,
                        singleLine = true,
                        textStyle = TextStyle(color = Palette.Text, fontSize = Type.Body),
                        cursorBrush = SolidColor(Palette.Accent),
                        keyboardOptions = KeyboardOptions(
                            keyboardType = KeyboardType.Text,
                            imeAction = ImeAction.Search,
                        ),
                        // Search on the IME key, which the field advertised and
                        // did not honour. Someone who types a query and presses
                        // the magnifier expects it to run now rather than 280ms
                        // later, and the debounce is for typing, not for a
                        // deliberate submit.
                        keyboardActions = KeyboardActions(onSearch = {
                            keyboard?.hide()
                            viewModel.submitSearch()
                        }),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }

            // The count is on the button, always, and that is the point of it.
            //
            // A filter still applied from three searches ago is the commonest
            // way a search screen lies: someone sets a price cap, searches
            // something else, gets nothing, and concludes the thing does not
            // exist. A number here is the cheapest possible fix.
            val active = state.filters.activeCount
            Box(
                Modifier
                    .border(
                        1.dp,
                        if (active > 0) Palette.Accent else Palette.Line,
                        RoundedCornerShape(50),
                    )
                    .clickable(onClick = viewModel::openFilters)
                    .defaultMinSize(minWidth = Space.Touch, minHeight = Space.Touch)
                    .padding(horizontal = Space.Medium),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = if (active > 0) {
                        stringResource(R.string.search_filters_n, active)
                    } else {
                        stringResource(R.string.search_filters)
                    },
                    color = if (active > 0) Palette.Accent else Palette.TextMuted,
                    fontSize = Type.Secondary,
                    fontWeight = if (active > 0) FontWeight.SemiBold else FontWeight.Normal,
                )
            }
        }

        when {
            state.loading && state.items.isEmpty() -> LoadingBlock()

            // Four states, not one. "Type something", "that failed", "nothing
            // matched" and a list are four different facts, and a single "No
            // results" reports a working search as broken to the person who has
            // not searched yet.
            !state.searched && state.error == null ->
                EmptyBlock(
                    message = stringResource(R.string.search_prompt),
                    hint = stringResource(R.string.search_prompt_hint),
                )

            state.error != null && state.items.isEmpty() ->
                FailureBlock(state.error!!, onRetry = viewModel::submitSearch)

            state.items.isEmpty() ->
                EmptyBlock(
                    message = stringResource(R.string.search_none, state.query),
                    hint = stringResource(R.string.search_none_hint),
                )

            else -> LazyColumn(state = listState, contentPadding = bottomInset()) {
                // A failed *refresh* over results that are still on screen is a
                // strip, not a replacement. The list the viewer is reading is
                // still the best information available, and throwing it away to
                // report that newer information did not arrive is destroying
                // something useful to say something they can already see.
                state.error?.let {
                    item { FailureStrip(it, onRetry = viewModel::submitSearch) }
                }
                item {
                    Text(
                        text = stringResource(R.string.search_count, state.total),
                        color = Palette.TextFaint,
                        fontSize = Type.Caption,
                        modifier = Modifier.padding(start = Space.Edge, bottom = Space.Tight),
                    )
                }
                items(state.items, key = { it.id }) { result ->
                    SearchRow(result) {
                        // Dismissed before navigating rather than after. A
                        // keyboard left up over the destination reads as the
                        // tap not having registered.
                        keyboard?.hide()
                        onOpen(result)
                    }
                }
                if (state.loadingMore) {
                    item { LoadingBlock(inline = true) }
                }
            }
        }
    }
}

@Composable
private fun SearchRow(r: SearchResult, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = Space.Edge, vertical = Space.Medium),
        horizontalArrangement = Arrangement.spacedBy(Space.Medium),
    ) {
        Poster(
            url = r.posterUrl.ifEmpty { null },
            lqip = r.lqip.ifEmpty { null },
            modifier = Modifier.size(width = 54.dp, height = 72.dp),
        )
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    // The server composes a title from year + make + model when
                    // the seller gave none, so this fallback is for a row that
                    // somehow has neither — never the common case.
                    text = r.title.ifEmpty { r.referenceNo },
                    color = Palette.Text,
                    fontSize = Type.Body,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier.weight(1f),
                )
                if (r.promoted) {
                    // Labelled, always. A promoted result that looks organic is
                    // what makes buyers stop trusting the order — at which point
                    // the placement the seller paid for is worth nothing.
                    Text(
                        text = stringResource(R.string.search_promoted),
                        color = Palette.Accent,
                        fontSize = Type.Micro,
                        fontWeight = FontWeight.Bold,
                        modifier = Modifier
                            .background(Palette.Accent.copy(alpha = 0.15f), RoundedCornerShape(50))
                            .padding(horizontal = 6.dp, vertical = 2.dp),
                    )
                }
            }
            Text(
                text = listOfNotNull(
                    if (r.mileageKm > 0) "${group(r.mileageKm.toLong())} km" else null,
                    r.regionalSpec.uppercase().ifEmpty { null },
                    r.city.ifEmpty { r.emirate }.ifEmpty { null },
                    r.distanceM?.let { distance(it) },
                ).joinToString(" · "),
                color = Palette.TextMuted,
                fontSize = Type.Secondary,
                modifier = Modifier.padding(top = Space.Hair),
            )
            Text(
                text = "${r.currency} ${group(r.priceMinor / 100)}",
                color = Palette.Accent,
                fontSize = Type.Body,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.padding(top = Space.Tight),
            )
        }
    }
}

/**
 * Distance, rounded to something a person would say.
 *
 * The old version divided metres by a thousand in integer arithmetic, so
 * everything closer than a kilometre read "0 km away" — which on the screen
 * where distance is the deciding column meant the nearest results looked like
 * missing data.
 */
private fun distance(metres: Long): String = when {
    metres < 1000 -> "$metres m away"
    metres < 10_000 -> "${metres / 1000}.${(metres % 1000) / 100} km away"
    else -> "${metres / 1000} km away"
}

// ─────────────────────────────────────────────────────────────────────────────
// Saved
// ─────────────────────────────────────────────────────────────────────────────

/**
 * What the viewer kept.
 *
 * A poster grid rather than a row list. A list of saved things spends most of
 * its width on words the viewer already read when they chose to keep the reel;
 * what tells six saved cars apart is the photograph, so the photograph gets
 * the screen. The words on each cell are still the vertical's, resolved
 * through the same SummarizePage the feed uses — a saved car and a saved
 * flash offer come through this one composable and it knows what neither of
 * them is.
 */
@Composable
fun SavedScreen(
    viewModel: MeViewModel,
    onOpen: (SavedItem) -> Unit,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.saved.collectAsState()
    LaunchedEffect(Unit) { viewModel.loadSaved() }

    Column(modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding()) {
        ScreenTitle(stringResource(R.string.nav_saved))
        when {
            state.loading && state.items.isEmpty() -> LoadingBlock()

            state.error != null && state.items.isEmpty() ->
                FailureBlock(state.error!!, onRetry = { viewModel.loadSaved(force = true) })

            state.items.isEmpty() ->
                EmptyBlock(
                    message = stringResource(R.string.saved_empty),
                    hint = stringResource(R.string.saved_empty_hint),
                )

            else -> {
                val bottom = bottomInset()
                LazyVerticalGrid(
                    columns = GridCells.Fixed(2),
                    contentPadding = PaddingValues(
                        start = Space.Edge,
                        end = Space.Edge,
                        bottom = bottom.calculateBottomPadding(),
                    ),
                    horizontalArrangement = Arrangement.spacedBy(Space.Medium),
                    verticalArrangement = Arrangement.spacedBy(Space.Medium),
                ) {
                    state.error?.let {
                        item(span = { GridItemSpan(maxLineSpan) }) {
                            FailureStrip(it, onRetry = { viewModel.loadSaved(force = true) })
                        }
                    }
                    items(state.items, key = { it.reelId }) { item ->
                        SavedCell(item) { onOpen(item) }
                    }
                }
            }
        }
    }
}

@Composable
private fun SavedCell(item: SavedItem, onClick: () -> Unit) {
    val fade = if (item.unavailable) 0.4f else 1f
    val summary = item.summary
    val title = summary?.title.orEmpty().ifEmpty { stringResource(R.string.saved_untitled) }

    Box(
        Modifier
            .fillMaxWidth()
            .aspectRatio(3f / 4f)
            .alpha(fade)
            .clip(RoundedCornerShape(Shape.Card))
            .clickable(enabled = !item.unavailable, onClick = onClick)
            // One announcement per cell rather than a fragment per line. A
            // screen reader moving through six saved cars should hear six
            // titles, not twenty-four text nodes.
            .semantics(mergeDescendants = true) { contentDescription = title },
    ) {
        Poster(
            url = item.posterUrl.ifEmpty { null },
            lqip = item.lqip.ifEmpty { null },
            corner = Shape.Card,
            modifier = Modifier.fillMaxSize(),
        )

        // The bottom gradient earns the text its legibility; the poster is an
        // arbitrary photograph and white on a bright one is not there.
        Box(
            Modifier
                .fillMaxSize()
                .background(
                    Brush.verticalGradient(
                        0.55f to Color.Transparent,
                        1.0f to Color.Black.copy(alpha = 0.7f),
                    )
                )
        )

        Column(
            Modifier
                .align(Alignment.BottomStart)
                .padding(Space.Medium),
        ) {
            Text(
                text = title,
                color = Palette.Text,
                fontSize = Type.Secondary,
                fontWeight = FontWeight.SemiBold,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            val price = summary?.priceMinor
            if (price != null && !summary.currency.isNullOrEmpty()) {
                Text(
                    text = "${summary.currency} ${group(price / 100)}${summary.priceSuffix}",
                    color = Palette.Accent,
                    fontSize = Type.Secondary,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.padding(top = Space.Hair),
                )
            }
            if (item.unavailable) {
                // Greyed rather than dropped. A saved car that sold is
                // something the viewer chose to keep, and silently deleting it
                // reads as the app losing their data.
                Text(
                    text = stringResource(R.string.saved_gone),
                    color = Palette.TextMuted,
                    fontSize = Type.Caption,
                    modifier = Modifier.padding(top = Space.Hair),
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
    val inbox by viewModel.inbox.collectAsState()
    LaunchedEffect(Unit) {
        viewModel.loadMyListings()
        // The unread count is the only reason to open the inbox, so it has to be
        // known before the viewer decides whether to. Loading it lazily meant
        // the row said "Messages" with no indication that three buyers were
        // waiting.
        viewModel.loadInbox()
    }

    Column(modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding()) {
        ScreenTitle(stringResource(R.string.nav_me))

        LazyColumn(contentPadding = bottomInset()) {
            item {
                RowLink(
                    label = stringResource(R.string.me_messages),
                    badge = inbox.items.sumOf { it.unread }.takeIf { it > 0 },
                    onClick = onOpenInbox,
                )
                Text(
                    text = stringResource(R.string.me_your_listings),
                    color = Palette.Text.copy(alpha = 0.45f),
                    fontSize = Type.Caption,
                    fontWeight = FontWeight.Medium,
                    modifier = Modifier.padding(
                        start = Space.Edge, top = Space.XL, bottom = Space.Tight,
                    ),
                )
            }

            when {
                state.loading && state.items.isEmpty() ->
                    item { LoadingBlock(inline = true) }

                state.error != null && state.items.isEmpty() ->
                    item {
                        FailureBlock(state.error!!, onRetry = { viewModel.loadMyListings(force = true) })
                    }

                state.items.isEmpty() ->
                    item {
                        EmptyBlock(
                            message = stringResource(R.string.me_no_listings),
                            hint = stringResource(R.string.me_no_listings_hint),
                        )
                    }

                else -> {
                    state.error?.let {
                        item { FailureStrip(it, onRetry = { viewModel.loadMyListings(force = true) }) }
                    }
                    items(state.items, key = { it.id }) { ListingRow(it) }
                }
            }

            item {
                Box(
                    Modifier
                        .padding(horizontal = Space.Edge, vertical = Space.XL)
                        .fillMaxWidth()
                        .border(1.dp, Palette.Line, RoundedCornerShape(Shape.Field))
                        .clickable(onClick = onSignOut)
                        .defaultMinSize(minHeight = Space.Touch)
                        .padding(vertical = 13.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        stringResource(R.string.me_sign_out),
                        color = Palette.Text.copy(alpha = 0.7f),
                        fontSize = Type.Secondary,
                    )
                }
            }
        }
    }
}

@Composable
private fun ListingRow(l: MyListing) {
    Row(
        Modifier
            .fillMaxWidth()
            .padding(horizontal = Space.Edge, vertical = 11.dp),
        horizontalArrangement = Arrangement.spacedBy(Space.Medium),
    ) {
        Poster(
            url = l.posterUrl.ifEmpty { null },
            lqip = l.lqip.ifEmpty { null },
            modifier = Modifier.size(width = 44.dp, height = 58.dp),
        )
        Column(Modifier.weight(1f)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = l.title.ifEmpty { l.referenceNo },
                    color = Palette.Text,
                    fontSize = Type.Body,
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
                color = Palette.TextMuted,
                fontSize = Type.Secondary,
                modifier = Modifier.padding(top = Space.Hair),
            )
            // The reel's state, when it is the thing holding the listing up. A
            // seller whose car is "pending review" while the video is still
            // transcoding is being told the wrong reason otherwise.
            reelNote(l)?.let {
                Text(
                    it,
                    color = Palette.TextFaint,
                    fontSize = Type.Caption,
                    modifier = Modifier.padding(top = Space.Hair),
                )
            }
        }
    }
}

@Composable
private fun StatusPill(l: MyListing) {
    val (label, colour) = when (l.status) {
        "active" -> stringResource(R.string.status_active) to Palette.Good
        "pending_review" -> stringResource(R.string.status_review) to Palette.Accent
        "draft" -> stringResource(R.string.status_draft) to Palette.TextFaint
        "rejected" -> stringResource(R.string.status_rejected) to Palette.Bad
        "sold" -> stringResource(R.string.status_sold) to Palette.TextFaint
        "paused" -> stringResource(R.string.status_paused) to Palette.TextFaint
        "expired" -> stringResource(R.string.status_expired) to Palette.TextFaint
        // A status this build predates still renders, as itself. Falling through
        // to nothing would make a new state invisible on the one screen that
        // exists to explain states.
        else -> l.status to Palette.TextFaint
    }
    Text(
        text = label.uppercase(),
        color = colour,
        fontSize = Type.Micro,
        fontWeight = FontWeight.Bold,
        modifier = Modifier
            // A soft tint rather than an outline: the pill is a status, not an
            // action, and the quieter shape keeps a row of them from reading
            // as a strip of buttons.
            .background(colour.copy(alpha = 0.15f), RoundedCornerShape(50))
            .padding(horizontal = 7.dp, vertical = 3.dp),
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
        color = Palette.Text,
        fontSize = Type.Title,
        fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = Space.Edge, top = Space.Medium, bottom = 10.dp),
    )
}

@Composable
private fun RowLink(label: String, badge: Int?, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .defaultMinSize(minHeight = Space.Touch)
            .padding(horizontal = Space.Edge, vertical = Space.Large),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Outlined.Email,
            contentDescription = null,
            tint = Palette.Text,
            modifier = Modifier.size(22.dp),
        )
        Text(
            label,
            color = Palette.Text,
            fontSize = Type.Body,
            modifier = Modifier.padding(start = Space.Medium),
        )
        if (badge != null) {
            Text(
                text = if (badge > 99) "99+" else badge.toString(),
                color = Color.White,
                fontSize = Type.Micro,
                fontWeight = FontWeight.Bold,
                modifier = Modifier
                    .padding(start = Space.Small)
                    .background(Palette.Accent, RoundedCornerShape(50))
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            )
        }
        Box(Modifier.weight(1f))
        Text(
            "›",
            color = Palette.TextFaint,
            fontSize = Type.Heading,
            // Decorative. Announcing "greater-than sign" after the row's own
            // label is noise.
            modifier = Modifier.clearAndSetSemantics { },
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
