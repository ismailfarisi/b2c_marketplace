package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.network.ApiException

/**
 * Cursor paging over the feed.
 *
 * Small, and every line of it is a rule the API asked for:
 *
 *  - **The cursor is opaque and never parsed.** It carries the page index and
 *    the exclusion set, and its format already changed once between Phase 2 and
 *    Phase 4. A client that inspected it would break on the next change.
 *  - **A stale cursor is not an error screen.** The server rejects one with 422
 *    rather than silently restarting; the correct client response is to throw
 *    the cursor away and reload from the top, which is what a viewer expects
 *    anyway when they come back to a feed after two days.
 *  - **A null `next_cursor` means the feed is exhausted, and that is a
 *    supported state.** PLAN-VIDEO-FIRST §10 is explicit that the product
 *    promise is "everything here is available to you right now", not endless
 *    scroll — "a feed that can be exhausted: finite and locally relevant beats
 *    infinite and irrelevant". A client that loops back to the top on exhaustion
 *    would be quietly rewriting that promise.
 */
class FeedPager(
    private val api: FeedApi,
    private val pageSize: Int = 10,
) {
    private val items = mutableListOf<FeedItem>()
    private var cursor: String? = null
    private var exhausted = false
    private var loading = false

    var rankingProfile: String? = null
        private set

    fun snapshot(): List<FeedItem> = items.toList()
    fun isExhausted(): Boolean = exhausted
    fun size(): Int = items.size

    /**
     * Loads the first page, discarding anything already held.
     */
    suspend fun refresh(lat: Double, lng: Double, categoryId: String? = null): List<FeedItem> {
        items.clear()
        cursor = null
        exhausted = false
        return loadNext(lat, lng, categoryId)
    }

    /**
     * Loads the next page.
     *
     * Returns the newly appended items — an empty list when exhausted or when a
     * load is already in flight. Re-entrancy matters here: a fast scroll fires
     * the prefetch trigger several times before the first response lands, and
     * without the guard the client requests the same page three times and pays
     * for it three times.
     */
    suspend fun loadNext(lat: Double, lng: Double, categoryId: String? = null): List<FeedItem> {
        if (exhausted || loading) return emptyList()
        loading = true
        try {
            val page = try {
                api.feed(lat, lng, cursor, categoryId, pageSize)
            } catch (e: ApiException) {
                if (!e.isStaleCursor) throw e
                // Drop the cursor and start again rather than surfacing an
                // error. The viewer's session is old, not broken.
                cursor = null
                items.clear()
                api.feed(lat, lng, null, categoryId, pageSize)
            }

            rankingProfile = page.rankingProfile
            cursor = page.nextCursor
            if (page.nextCursor == null || page.items.isEmpty()) exhausted = true

            // De-duplicated on append even though the server already excludes
            // seen reels via the cursor. Belt and braces: a duplicate id in a
            // lazy list is a crash on Android, not a cosmetic problem, and the
            // cost of the check is a set over one page.
            val known = items.mapTo(HashSet()) { it.id }
            val fresh = page.items.filter { known.add(it.id) }
            items += fresh
            return fresh
        } finally {
            loading = false
        }
    }

    /**
     * How close to the end the viewer must be before the next page is fetched.
     *
     * Equal to the prefetch window, deliberately. Fetching earlier means paying
     * for pages nobody reaches; later means the viewer hits the end of the list
     * and the feed stalls at exactly the moment it should feel effortless.
     */
    fun shouldLoadMore(visibleIndex: Int): Boolean =
        !exhausted && !loading && visibleIndex >= items.size - PREFETCH_PAGES_AT

    companion object {
        const val PREFETCH_PAGES_AT = 3
    }
}
