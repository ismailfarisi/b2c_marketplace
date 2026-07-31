package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.toFailure
import ae.marketplace.mobile.model.Vertical
import ae.marketplace.mobile.model.ClientEvent
import ae.marketplace.mobile.events.EventQueue
import ae.marketplace.mobile.playback.PlaybackCoordinator
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

data class FeedUiState(
    val items: List<FeedItem> = emptyList(),
    val loading: Boolean = false,
    val exhausted: Boolean = false,
    val error: String? = null,
    val rankingProfile: String? = null,
    /**
     * The tab the viewer tapped. Highlighted immediately, before the page it
     * asks for has landed — a tab row that waits for a response feels broken on
     * a slow connection, and this is the one piece of feed state where being
     * optimistic is free.
     */
    val surface: FeedSurface = FeedSurface.ForYou,
    /**
     * The surface the server says it served. Differs from [surface] only when
     * this build asked for a tab the server has retired, and then it is the
     * honest label for what is on screen.
     */
    val servedSurface: FeedSurface = FeedSurface.ForYou,
    /**
     * Why the list is empty, when it is and the server said so.
     *
     * Kept apart from [error] on purpose. "Nothing is live right now" is a true
     * statement about a working product; rendering it through the error path
     * would tell the viewer the app is broken, and that decides whether they try
     * the next tab or close it.
     */
    val emptyReason: FeedEmptyReason? = null,
    /**
     * Server clock minus device clock, sampled once when this surface's first
     * page landed. Passed to ReelClock.face on every tick; see its note on why
     * re-sampling per tick freezes every countdown in the feed.
     */
    val clockSkewMillis: Long = 0L,
    /**
     * The verticals the server says this feed can be filtered by, in the order
     * it gave them. Empty until the first fetch lands, and empty forever in a
     * deployment with no providers registered — in which case the filter row
     * should not be drawn at all rather than drawn with one "All" chip.
     */
    val verticals: List<Vertical> = emptyList(),
    /** The selected vertical's key, or null for all of them. */
    val selectedVertical: String? = null,
    /** Where the feed is being served for. Never null — see LocationChoice. */
    val lat: Double = 0.0,
    val lng: Double = 0.0,
    /**
     * A label for the current point, when it came from a named place. Empty when
     * it came from the device, because the client has no reverse geocoder and
     * "25.08, 55.14" is not a location a person recognises.
     */
    val locationLabel: String = "",
)

/**
 * One reel, opened deliberately rather than served.
 *
 * [reelId] outlives [item] on purpose: it is set the moment the viewer taps,
 * before anything has been fetched, so a late response for a reel they have
 * since dismissed can be recognised and dropped.
 */
data class FocusedReelState(
    val reelId: String? = null,
    val item: FeedItem? = null,
    val loading: Boolean = false,
    val error: Failure? = null,
) {
    val open: Boolean get() = reelId != null
}

/**
 * The feed's state, shared.
 *
 * `androidx.lifecycle.ViewModel` is multiplatform as of 2.8, so this class and
 * its scope are common code and SwiftUI observes the same [StateFlow] Compose
 * does. That is worth more than it sounds: paging, error handling, the
 * exhausted state and the playback hand-off are the parts most likely to differ
 * subtly between two hand-written implementations, and the difference shows up
 * as "the iOS feed double-counts views" three months later.
 *
 * What is *not* here: anything that draws. The list, the player surface and the
 * CTA sheet are platform code, because that is where the performance the whole
 * KMM decision was made for actually lives.
 */
class FeedViewModel(
    private val pager: FeedPager,
    private val playback: PlaybackCoordinator,
    private val events: EventQueue,
    private val api: FeedApi,
) : ViewModel() {

    private val _state = MutableStateFlow(FeedUiState())
    val state: StateFlow<FeedUiState> = _state.asStateFlow()

    private val _focused = MutableStateFlow(FocusedReelState())

    /**
     * A single reel opened over the feed, or nothing.
     *
     * Separate from [state] rather than a nullable field on it, because every
     * consumer of the feed's list state would otherwise recompose whenever this
     * changed — including the pager, which has no interest in it.
     */
    val focused: StateFlow<FocusedReelState> = _focused.asStateFlow()

    private var lat: Double = 0.0
    private var lng: Double = 0.0
    private var categoryId: String? = null

    fun start(lat: Double, lng: Double, categoryId: String? = null, locationLabel: String = "") {
        this.lat = lat
        this.lng = lng
        this.categoryId = categoryId
        _state.value = _state.value.copy(lat = lat, lng = lng, locationLabel = locationLabel)
        events.start()
        loadFilters()
        refresh()
    }

    /**
     * Fetches the filter row once.
     *
     * Failure is silent and the row simply does not appear. A filter list is
     * navigation furniture — surfacing its failure as an error over a feed that
     * loaded perfectly well would report a broken app to someone holding a
     * working one.
     */
    private fun loadFilters() {
        viewModelScope.launch {
            runCatching { api.filters() }
                .onSuccess { _state.value = _state.value.copy(verticals = it.verticals) }
        }
    }

    /**
     * The viewer picked a vertical, or cleared it by passing null.
     *
     * A new filter is a new feed, so the pager resets — same reasoning as
     * [onSurfaceSelected]: the cursor carries an exclusion set built for the
     * query that produced it, and reusing it would page one feed with another's
     * memory.
     */
    fun onVerticalSelected(key: String?) {
        if (_state.value.selectedVertical == key) return
        playback.onPause()
        _state.value = _state.value.copy(
            selectedVertical = key,
            items = emptyList(),
            emptyReason = null,
            exhausted = false,
        )
        refresh()
    }

    /**
     * The viewer moved the feed somewhere else.
     *
     * The whole product is local discovery, so this is not a filter over the
     * current results — it is a different feed, and every candidate source is
     * geo-scoped. Hence the same reset.
     */
    fun onLocationSelected(lat: Double, lng: Double, label: String) {
        if (this.lat == lat && this.lng == lng) return
        this.lat = lat
        this.lng = lng
        playback.onPause()
        _state.value = _state.value.copy(
            lat = lat,
            lng = lng,
            locationLabel = label,
            items = emptyList(),
            emptyReason = null,
            exhausted = false,
        )
        refresh()
    }

    /**
     * The viewer tapped a tab.
     *
     * A surface change is a new feed, not a filter over the current one, so the
     * pager is reset rather than re-sorted — the cursor carries an exclusion set
     * built for the surface that produced it, and reusing it would page one feed
     * with another's memory.
     *
     * Playback stops first. Without that, the reel that was on screen keeps
     * playing under the new tab's first item for as long as the request takes,
     * which reads as the app having lost track of what it is showing.
     *
     * Tapping the tab you are already on is a no-op rather than a refresh. On a
     * vertical pager the tab row sits under the viewer's thumb, and making an
     * accidental re-tap throw away their place in the feed is a bad trade for
     * the one person in a hundred who meant it as a reload.
     */
    fun onSurfaceSelected(surface: FeedSurface) {
        if (_state.value.surface == surface) return
        playback.onPause()
        // Highlighted before the page lands. The list below is cleared in the
        // same update so the viewer never sees the previous surface's reels
        // sitting under the newly selected tab.
        _state.value = _state.value.copy(
            surface = surface,
            items = emptyList(),
            emptyReason = null,
            exhausted = false,
        )
        refresh()
    }

    fun refresh() {
        val surface = _state.value.surface
        val vertical = _state.value.selectedVertical
        viewModelScope.launch {
            _state.value = _state.value.copy(loading = true, error = null)
            runCatching { pager.refresh(lat, lng, categoryId, surface, vertical) }
                .onSuccess { emit() }
                .onFailure { e ->
                    _state.value = _state.value.copy(loading = false, error = e.message)
                }
        }
    }

    /**
     * Called by the platform list when the visible page changes.
     *
     * Both effects hang off this one call — playback and paging — because they
     * are driven by the same fact and splitting them means two listeners that
     * can disagree about which reel is visible.
     */
    fun onVisible(index: Int) {
        val items = _state.value.items
        playback.onVisible(index, items)

        if (pager.shouldLoadMore(index)) {
            val surface = _state.value.surface
            val vertical = _state.value.selectedVertical
            viewModelScope.launch {
                runCatching { pager.loadNext(lat, lng, categoryId, surface, vertical) }
                    .onSuccess { if (it.isNotEmpty()) emit() else emit() }
                    .onFailure { /* Keep the feed on screen. The next swipe retries. */ }
            }
        }
    }

    /**
     * Optimistic like.
     *
     * The counter is nudged locally and the request is fired without waiting.
     * The API's own note explains why: counters lag by up to the flush interval
     * anyway, so "a badge that is 3% low for ten seconds costs nothing, while a
     * heart that un-fills when you scroll away and back is immediately wrong to
     * the one person who knows the answer."
     */
    fun toggleLike(reelId: String) {
        val items = _state.value.items
        val index = items.indexOfFirst { it.id == reelId }
        if (index < 0) return

        val item = items[index]
        val liked = !item.viewer.liked
        val updated = items.toMutableList()
        updated[index] = item.copy(
            viewer = item.viewer.copy(liked = liked),
            counters = item.counters.copy(
                likes = (item.counters.likes + if (liked) 1 else -1).coerceAtLeast(0)
            ),
        )
        _state.value = _state.value.copy(items = updated)

        events.enqueue(
            ClientEvent(
                type = if (liked) ClientEvent.LIKE else ClientEvent.UNLIKE,
                reelId = reelId,
                occurredAt = events.timestamp(),
            )
        )

        viewModelScope.launch {
            runCatching { if (liked) api.like(reelId) else api.unlike(reelId) }
                .onFailure {
                    // Put it back. A like that silently did not happen is worse
                    // than one that visibly failed, because the viewer believes
                    // they have saved something.
                    val current = _state.value.items.toMutableList()
                    val i = current.indexOfFirst { it.id == reelId }
                    if (i >= 0) current[i] = item
                    _state.value = _state.value.copy(items = current)
                }
        }
    }

    /**
     * A CTA press.
     *
     * The click is recorded *before* navigation, and it is recorded even when
     * the target turns out to be unavailable. Phase 3 built the funnel around
     * `action_click` as the denominator for conversion; a client that only
     * reports successful navigations makes the conversion rate look better than
     * it is by exactly the number of dead CTAs in the feed.
     */
    fun onActionClick(actionId: String, reelId: String) {
        events.enqueue(
            ClientEvent(
                type = ClientEvent.ACTION_CLICK,
                reelId = reelId,
                occurredAt = events.timestamp(),
                properties = mapOf("action_id" to actionId),
            )
        )
        viewModelScope.launch { runCatching { api.clickAction(actionId) } }
    }

    /**
     * The player a surface should attach to, or null when the reel is outside
     * the prefetch window.
     *
     * The only place platform UI reaches into shared state, and it hands back
     * the seven-method [ae.marketplace.mobile.playback.ReelPlayer] rather than a
     * decoder. Each platform widens it to its own type through an extension in
     * its own source set — `asExoPlayer()` / `asAvPlayer()` — so neither type
     * ever appears in common code.
     */
    fun playerFor(reelId: String) = playback.playerFor(reelId)

    // ── One reel, on its own ─────────────────────────────────────────────────

    /**
     * Opens a single reel over the feed — the destination for a Saved row or a
     * search result.
     *
     * # Why this lives on the feed's state holder
     *
     * It could have been its own view model, and that was the first instinct.
     * It would also have meant a second [PlaybackCoordinator], which owns a
     * pool of hardware decoders. On a mid-range Android device those are a
     * genuinely scarce resource: a second pool held open behind the first is
     * how the *next* video anywhere in the app fails to start, with no error
     * that names the cause.
     *
     * So the focused reel borrows this coordinator. It is the same machinery,
     * one item long, and the feed list underneath is untouched — which is the
     * other half of the requirement, because a viewer who dismisses this must
     * come back to where they were rather than to the top.
     */
    fun openReel(reelId: String) {
        if (_focused.value.reelId == reelId && _focused.value.item != null) return

        // The list is paused before the fetch, not after it. The coordinator
        // would otherwise keep the feed's current reel playing — audio and all
        // — underneath a screen that has already visually replaced it.
        playback.onPause()

        _focused.value = FocusedReelState(reelId = reelId, loading = true)
        viewModelScope.launch {
            runCatching { api.reel(reelId, lat.takeIf { it != 0.0 }, lng.takeIf { it != 0.0 }) }
                .onSuccess { item ->
                    // Guarded against a stale response: a viewer can dismiss
                    // and open a different reel while the first is in flight,
                    // and without this the first one wins by arriving second.
                    if (_focused.value.reelId != reelId) return@onSuccess
                    _focused.value = FocusedReelState(reelId = reelId, item = item)
                    playback.onVisible(0, listOf(item))
                }
                .onFailure { e ->
                    if (_focused.value.reelId != reelId) return@onFailure
                    _focused.value = FocusedReelState(reelId = reelId, error = e.toFailure())
                }
        }
    }

    /**
     * Closes it and hands playback back to the list.
     *
     * `onVisible` with the pager's own index rather than `onForeground`,
     * because the coordinator's notion of "current" was moved to the focused
     * reel and has to be moved back explicitly — otherwise the feed resumes
     * with the wrong reel considered active and credits its watch time to a
     * reel nobody is looking at.
     */
    fun closeReel(returnToIndex: Int) {
        _focused.value = FocusedReelState()
        val items = _state.value.items
        if (items.isNotEmpty()) {
            playback.onVisible(returnToIndex.coerceIn(0, items.lastIndex), items)
        }
    }

    /** Retries a failed fetch, keeping the same target. */
    fun retryReel() {
        val id = _focused.value.reelId ?: return
        _focused.value = FocusedReelState()
        openReel(id)
    }

    fun onAppBackgrounded() {
        playback.onPause()
        // Flushed here, not only on the timer. The last batch of a session is
        // the one containing whatever the viewer converted on.
        viewModelScope.launch { runCatching { events.flush() } }
    }

    fun onAppForegrounded() {
        playback.onResume()
    }

    override fun onCleared() {
        playback.release()
        events.stop()
        super.onCleared()
    }

    private fun emit() {
        _state.value = _state.value.copy(
            items = pager.snapshot(),
            loading = false,
            exhausted = pager.isExhausted(),
            rankingProfile = pager.rankingProfile,
            servedSurface = pager.servedSurface,
            emptyReason = pager.emptyReason,
            clockSkewMillis = pager.clockSkewMillis,
            error = null,
        )
    }
}
