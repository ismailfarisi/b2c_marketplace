package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.FeedItem
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
)

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

    private var lat: Double = 0.0
    private var lng: Double = 0.0
    private var categoryId: String? = null

    fun start(lat: Double, lng: Double, categoryId: String? = null) {
        this.lat = lat
        this.lng = lng
        this.categoryId = categoryId
        events.start()
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            _state.value = _state.value.copy(loading = true, error = null)
            runCatching { pager.refresh(lat, lng, categoryId) }
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
            viewModelScope.launch {
                runCatching { pager.loadNext(lat, lng, categoryId) }
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
            error = null,
        )
    }
}
