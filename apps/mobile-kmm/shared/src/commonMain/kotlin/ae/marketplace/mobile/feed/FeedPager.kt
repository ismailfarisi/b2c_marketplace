package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.network.ApiException
import kotlin.time.Clock
import kotlin.time.ExperimentalTime

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
@OptIn(ExperimentalTime::class)
class FeedPager(
    private val api: FeedApi,
    private val pageSize: Int = 10,
    /**
     * The device clock, injected so the skew sample is testable. Reading it
     * directly here would make "does a fast device close the auction early"
     * a question only a device can answer.
     */
    private val nowMillis: () -> Long = { Clock.System.now().toEpochMilliseconds() },
) {
    private val items = mutableListOf<FeedItem>()
    private var cursor: String? = null
    private var exhausted = false
    private var loading = false
    private var skewSampled = false

    /**
     * Which refresh the pager is currently serving.
     *
     * Bumped by [refresh] and captured by [loadNext] at entry. A response that
     * comes back under a stale generation is dropped on the floor — it belongs
     * to a surface the viewer has left, and there is no correct place to put it.
     *
     * A generation counter rather than cancellation because the pager holds no
     * scope: it is a suspending object called from whatever coroutine the
     * caller owns, and the caller cancelling its own job must not be the only
     * thing standing between a stale page and the wrong tab.
     */
    private var generation = 0

    var rankingProfile: String? = null
        private set

    /**
     * What the server said it served, and why the page was empty.
     *
     * Held here rather than derived by the caller because the pager is what
     * touches the response. `servedSurface` can differ from the one requested —
     * the server defaults a surface it does not recognise — and highlighting the
     * requested tab instead would mislabel the page on any build that outlives a
     * tab.
     */
    var servedSurface: FeedSurface = FeedSurface.ForYou
        private set

    var emptyReason: FeedEmptyReason? = null
        private set

    /**
     * The server clock minus this device's, sampled when the first page of this
     * surface landed and then held for as long as the surface is on screen.
     *
     * Sampled once, deliberately. ReelClock.skew's note has the detail: recompute
     * it per tick and every countdown in the feed freezes at the value it had
     * when the page loaded. Re-sampled on refresh, which is the only moment the
     * device could have resynchronised without the app noticing.
     */
    var clockSkewMillis: Long = 0L
        private set

    fun snapshot(): List<FeedItem> = items.toList()
    fun isExhausted(): Boolean = exhausted
    fun size(): Int = items.size

    /**
     * Loads the first page, discarding anything already held.
     *
     * Abandons any page still in flight rather than waiting for it or refusing
     * to start. Both alternatives were tried and both are wrong: waiting makes
     * a tab tap take as long as the request the viewer just gave up on, and
     * refusing left the new surface permanently blank, because `loadNext`
     * returned immediately on the abandoned load's `loading` flag and no
     * request ever went out.
     */
    suspend fun refresh(
        lat: Double,
        lng: Double,
        categoryId: String? = null,
        surface: FeedSurface = FeedSurface.ForYou,
        subjectType: String? = null,
    ): List<FeedItem> {
        generation++
        items.clear()
        cursor = null
        exhausted = false
        emptyReason = null
        // Released here rather than by the abandoned load, which may be parked
        // on a socket for the length of a timeout and holds the only thing
        // stopping the fetch below.
        loading = false
        // The device could have resynchronised its clock since the last page,
        // and a refresh is the only moment the app can notice. Re-sampled, as
        // clockSkewMillis has always claimed.
        skewSampled = false
        return loadNext(lat, lng, categoryId, surface, subjectType)
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
    suspend fun loadNext(
        lat: Double,
        lng: Double,
        categoryId: String? = null,
        surface: FeedSurface = FeedSurface.ForYou,
        subjectType: String? = null,
    ): List<FeedItem> {
        if (exhausted || loading) return emptyList()
        loading = true
        val mine = generation
        try {
            val page = try {
                api.feed(lat, lng, cursor, categoryId, pageSize, surface, subjectType)
            } catch (e: ApiException) {
                if (!e.isStaleCursor) throw e
                // Drop the cursor and start again rather than surfacing an
                // error. The viewer's session is old, not broken.
                //
                // Guarded, because this branch *writes* before it retries: a
                // stale load clearing the list here would blank whatever the
                // current surface has already put on screen.
                if (mine != generation) return emptyList()
                cursor = null
                items.clear()
                api.feed(lat, lng, null, categoryId, pageSize, surface, subjectType)
            }

            // A refresh happened while this was in flight. Everything below
            // writes shared state — cursor, exhaustion, the list itself — and
            // all of it describes a surface the viewer is no longer looking at.
            if (mine != generation) return emptyList()

            rankingProfile = page.rankingProfile
            servedSurface = FeedSurface.fromWire(page.surface)
            emptyReason = FeedEmptyReason.fromWire(page.emptyReason)
            cursor = page.nextCursor
            if (page.nextCursor == null || page.items.isEmpty()) exhausted = true

            // De-duplicated on append even though the server already excludes
            // seen reels via the cursor. Belt and braces: a duplicate id in a
            // lazy list is a crash on Android, not a cosmetic problem, and the
            // cost of the check is a set over one page.
            val known = items.mapTo(HashSet()) { it.id }
            val fresh = page.items.filter { known.add(it.id) }

            // Skew from the first page that carried a signal, and only then.
            // Later pages are ignored: a second sample taken thirty seconds into
            // a session would shift every countdown already on screen by the
            // difference between two measurements of the same thing.
            if (!skewSampled) {
                fresh.firstOrNull { it.timeSignal != null }?.timeSignal?.let {
                    clockSkewMillis = ReelClock.skew(it, nowMillis())
                    skewSampled = true
                }
            }

            items += fresh
            return fresh
        } finally {
            // Only if this load is still the current one. A stale load
            // finishing — or throwing — must not clear the guard belonging to
            // the refresh that superseded it, or the next prefetch would run
            // concurrently with a page already in flight.
            if (mine == generation) loading = false
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
