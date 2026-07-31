package ae.marketplace.mobile.me

import ae.marketplace.mobile.model.Conversation
import ae.marketplace.mobile.model.Message
import ae.marketplace.mobile.model.MyListing
import ae.marketplace.mobile.model.SavedItem
import ae.marketplace.mobile.model.SearchResult
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

data class SavedUiState(
    val items: List<SavedItem> = emptyList(),
    val loading: Boolean = false,
    val loaded: Boolean = false,
    val error: String? = null,
)

data class MyListingsUiState(
    val items: List<MyListing> = emptyList(),
    val loading: Boolean = false,
    val loaded: Boolean = false,
    val error: String? = null,
)

data class SearchUiState(
    val query: String = "",
    val items: List<SearchResult> = emptyList(),
    val total: Long = 0,
    val loading: Boolean = false,
    /** True once a search has actually run, so "no results" can be told from "not searched yet". */
    val searched: Boolean = false,
    val error: String? = null,
)

data class InboxUiState(
    val items: List<Conversation> = emptyList(),
    val loading: Boolean = false,
    val error: String? = null,
)

data class ThreadUiState(
    val conversationId: String = "",
    val title: String = "",
    val messages: List<Message> = emptyList(),
    val draft: String = "",
    val loading: Boolean = false,
    val sending: Boolean = false,
    val error: String? = null,
)

/**
 * The signed-in viewer's own screens.
 *
 * One view model across Saved, Me, Search and messaging, deliberately. They are
 * four lists behind four tabs of one shell, they are all cheap, and separating
 * them would mean four objects with four lifecycles for a screen whose whole
 * job is to stay loaded while the viewer flicks between tabs.
 *
 * Each list keeps a `loaded` flag so re-entering a tab does not re-fetch. A
 * pull-to-refresh calls the loader again explicitly, which is the only thing
 * that should.
 */
class MeViewModel(private val api: MeApi) : ViewModel() {

    private val _saved = MutableStateFlow(SavedUiState())
    val saved: StateFlow<SavedUiState> = _saved.asStateFlow()

    private val _listings = MutableStateFlow(MyListingsUiState())
    val listings: StateFlow<MyListingsUiState> = _listings.asStateFlow()

    private val _search = MutableStateFlow(SearchUiState())
    val search: StateFlow<SearchUiState> = _search.asStateFlow()

    private val _inbox = MutableStateFlow(InboxUiState())
    val inbox: StateFlow<InboxUiState> = _inbox.asStateFlow()

    private val _thread = MutableStateFlow(ThreadUiState())
    val thread: StateFlow<ThreadUiState> = _thread.asStateFlow()

    private var searchJob: Job? = null
    private var lat: Double? = null
    private var lng: Double? = null

    /** Where the viewer is, so search can sort by distance. Optional. */
    fun setLocation(lat: Double, lng: Double) {
        this.lat = lat
        this.lng = lng
    }

    // ── Saved ────────────────────────────────────────────────────────────────

    fun loadSaved(force: Boolean = false) {
        if (_saved.value.loading || (_saved.value.loaded && !force)) return
        _saved.value = _saved.value.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { api.saves() }
                .onSuccess { _saved.value = SavedUiState(items = it.items, loaded = true) }
                .onFailure { e -> _saved.value = _saved.value.copy(loading = false, error = e.message) }
        }
    }

    // ── My listings ──────────────────────────────────────────────────────────

    fun loadMyListings(force: Boolean = false) {
        if (_listings.value.loading || (_listings.value.loaded && !force)) return
        _listings.value = _listings.value.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { api.myListings() }
                .onSuccess { _listings.value = MyListingsUiState(items = it.items, loaded = true) }
                .onFailure { e -> _listings.value = _listings.value.copy(loading = false, error = e.message) }
        }
    }

    // ── Search ───────────────────────────────────────────────────────────────

    /**
     * Debounced, cancelling the previous request.
     *
     * Same reasoning as the sell form's lookup: without the cancel, a slow
     * response for "pat" can land after a fast one for "patrol" and replace the
     * right results with stale ones — which reads as the app ignoring what you
     * typed.
     */
    fun onQueryChanged(query: String) {
        _search.value = _search.value.copy(query = query)
        searchJob?.cancel()
        if (query.isBlank()) {
            _search.value = _search.value.copy(items = emptyList(), total = 0, searched = false)
            return
        }
        searchJob = viewModelScope.launch {
            delay(SEARCH_DEBOUNCE_MS)
            runSearch(query)
        }
    }

    fun submitSearch() {
        searchJob?.cancel()
        val q = _search.value.query
        if (q.isBlank()) return
        searchJob = viewModelScope.launch { runSearch(q) }
    }

    private suspend fun runSearch(query: String) {
        _search.value = _search.value.copy(loading = true, error = null)
        runCatching { api.search(query = query, lat = lat, lng = lng) }
            .onSuccess {
                // The query is re-read from state rather than closed over, so a
                // result that lands after the box was cleared does not repopulate
                // it.
                _search.value = _search.value.copy(
                    items = it.items, total = it.total,
                    loading = false, searched = true, error = null,
                )
            }
            .onFailure { e ->
                _search.value = _search.value.copy(loading = false, searched = true, error = e.message)
            }
    }

    // ── Messaging ────────────────────────────────────────────────────────────

    fun loadInbox() {
        _inbox.value = _inbox.value.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { api.inbox() }
                .onSuccess { _inbox.value = InboxUiState(items = it.items) }
                .onFailure { e -> _inbox.value = _inbox.value.copy(loading = false, error = e.message) }
        }
    }

    fun openThread(conversationId: String, title: String) {
        _thread.value = ThreadUiState(conversationId = conversationId, title = title, loading = true)
        viewModelScope.launch {
            runCatching { api.thread(conversationId) }
                .onSuccess { _thread.value = _thread.value.copy(messages = it.items, loading = false) }
                .onFailure { e -> _thread.value = _thread.value.copy(loading = false, error = e.message) }
            // Marking read is fire-and-forget: a failure costs an unread badge,
            // and blocking the thread on it would be a worse trade.
            runCatching { api.markRead(conversationId) }
            loadInbox()
        }
    }

    /**
     * Opens the thread for a listing — the reel CTA's destination.
     *
     * The reel and action ids travel with it. They are what lets someone answer
     * "did the feed produce this lead"; without them every conversion this
     * product exists to create looks organic.
     */
    fun inquire(listingId: String, title: String, reelId: String?, actionId: String?) {
        _thread.value = ThreadUiState(title = title, loading = true)
        viewModelScope.launch {
            runCatching { api.inquire(listingId, reelId, actionId) }
                .onSuccess { openThread(it.conversationId, title) }
                .onFailure { e -> _thread.value = _thread.value.copy(loading = false, error = e.message) }
        }
    }

    fun onDraftChanged(text: String) {
        _thread.value = _thread.value.copy(draft = text)
    }

    /**
     * Sends, and renders whatever actually happened.
     *
     * There is no optimistic bubble. A message can come back `held` — under
     * review and undelivered — or `redacted`, with a phone number stripped out.
     * Drawing the text the sender typed and moving on would misreport both, and
     * a sender who believes a held message arrived concludes they are being
     * ignored and goes looking for a way off the platform.
     */
    fun sendDraft() {
        val s = _thread.value
        val body = s.draft.trim()
        if (body.isEmpty() || s.sending || s.conversationId.isEmpty()) return

        _thread.value = s.copy(sending = true, error = null)
        viewModelScope.launch {
            runCatching { api.send(s.conversationId, body) }
                .onSuccess { sent ->
                    _thread.value = _thread.value.copy(
                        messages = _thread.value.messages + sent,
                        draft = "",
                        sending = false,
                    )
                }
                .onFailure { e ->
                    // The draft stays in the box. Losing what someone typed
                    // because the network dropped is the single most annoying
                    // failure a messaging screen has.
                    _thread.value = _thread.value.copy(sending = false, error = e.message)
                }
        }
    }

    fun closeThread() {
        _thread.value = ThreadUiState()
    }

    private companion object {
        const val SEARCH_DEBOUNCE_MS = 280L
    }
}
