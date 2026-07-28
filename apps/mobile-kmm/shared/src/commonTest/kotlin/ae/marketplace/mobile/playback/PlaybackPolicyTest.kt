package ae.marketplace.mobile.playback

import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.model.Author
import ae.marketplace.mobile.model.Rendition
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

/**
 * These tests run on Android *and* iOS from one source set. That is the concrete
 * payoff of the KMM decision: the egress policy from PLAN-VIDEO-FIRST §9 has one
 * implementation, and both platforms prove it holds.
 */
class PlaybackPolicyTest {

    private fun ladder(vararg labels: String) = labels.mapIndexed { i, l ->
        Rendition(label = l, width = 640 + i * 320, height = 360 + i * 180,
            bitrateKbps = 600 + i * 800, playlistUrl = "https://cdn/$l.m3u8")
    }

    private fun item(durationMs: Int) = FeedItem(
        id = "r1", author = Author(type = "vendor_branch", id = "b1"), durationMs = durationMs,
    )

    @Test
    fun picks_the_best_rendition_within_the_cap() {
        val chosen = PlaybackPolicy.select(ladder("360p", "540p", "720p"), metered = false)
        assertEquals("720p", chosen?.label)
    }

    @Test
    fun never_plays_above_the_cap() {
        // The server does not produce 1080p — "roughly doubles egress for a
        // difference nobody sees on a phone". If it ever starts, an old build
        // must not opt itself into that bill.
        val withFuture = ladder("540p", "720p") + Rendition(
            label = "1080p", width = 1920, height = 1080,
            bitrateKbps = 4500, playlistUrl = "https://cdn/1080p.m3u8",
        )
        val chosen = PlaybackPolicy.select(withFuture, metered = false)
        assertEquals("720p", chosen?.label)
    }

    @Test
    fun drops_a_step_on_a_metered_connection() {
        // Not to save the platform money — the egress is already committed — but
        // to save a prepaid data plan, which is a real GCC constraint.
        val chosen = PlaybackPolicy.select(ladder("360p", "540p", "720p"), metered = true)
        assertEquals("540p", chosen?.label)
    }

    @Test
    fun falls_back_to_the_lowest_when_nothing_fits() {
        // A transcode that only produced 720p, on a metered connection. A reel
        // that starts beats one that buffers.
        val chosen = PlaybackPolicy.select(ladder("720p"), metered = true)
        assertEquals("720p", chosen?.label)
    }

    @Test
    fun no_renditions_is_not_a_crash() {
        assertNull(PlaybackPolicy.select(emptyList(), metered = false))
    }

    // ── the prefetch window ──────────────────────────────────────────────────

    @Test
    fun prefetches_exactly_two_ahead() {
        // The number is from the plan and it is a direct multiplier on egress.
        // A test rather than a comment because the constant is the kind of thing
        // someone raises to "make scrolling smoother" without seeing the bill.
        assertEquals(PlaybackPolicy.PREFETCH_AHEAD, 2)

        val window = PlaybackPolicy.window(current = 5, size = 20)
        assertTrue(6 in window && 7 in window, "should warm the next two: $window")
        assertFalse(8 in window, "must not warm a third: $window")
    }

    @Test
    fun the_visible_reel_is_always_first() {
        // So a caller that can only prepare one thing prepares the right thing.
        assertEquals(5, PlaybackPolicy.window(current = 5, size = 20).first())
    }

    @Test
    fun keeps_one_behind_warm() {
        // Scrolling back up is common, and re-preparing a reel someone was
        // watching four seconds ago is the most obviously wasteful buffer in the
        // product.
        assertTrue(4 in PlaybackPolicy.window(current = 5, size = 20))
        assertFalse(3 in PlaybackPolicy.window(current = 5, size = 20))
    }

    @Test
    fun the_window_is_clamped_at_both_ends() {
        assertEquals(listOf(0, 1, 2), PlaybackPolicy.window(current = 0, size = 3))
        assertEquals(listOf(2, 1), PlaybackPolicy.window(current = 2, size = 3))
        assertTrue(PlaybackPolicy.window(current = 0, size = 0).isEmpty())
        // A visible index past the end is a race between a refresh and a scroll,
        // not a programming error. Clamping beats throwing on the scroll path.
        assertEquals(4, PlaybackPolicy.window(current = 99, size = 5).first())
    }

    @Test
    fun the_window_never_exceeds_the_pool() {
        // The pool is sized from these constants. If the window could be larger,
        // the pool would evict a player it is about to be asked for.
        val poolCapacity = PlaybackPolicy.PREFETCH_AHEAD + PlaybackPolicy.KEEP_BEHIND + 1
        for (size in 1..30) {
            for (i in 0 until size) {
                assertTrue(
                    PlaybackPolicy.window(i, size).size <= poolCapacity,
                    "window at $i of $size exceeds pool capacity $poolCapacity",
                )
            }
        }
    }

    // ── what counts ──────────────────────────────────────────────────────────

    @Test
    fun a_view_is_two_seconds() {
        // The server applies this too. Two implementations of the word "view" is
        // how a ranking model learns the wrong thing — this constant exists so
        // the client can label an event, never to filter one out.
        assertFalse(PlaybackPolicy.isView(1_999))
        assertTrue(PlaybackPolicy.isView(2_000))
    }

    @Test
    fun completion_allows_for_the_last_half_second() {
        // A trailing keyframe or a swipe on the final frames is a legitimate
        // completion. Demanding the exact end understates completion rate on
        // precisely the best-performing content.
        val reel = item(durationMs = 20_000)
        assertTrue(PlaybackPolicy.isCompleted(19_000, reel))
        assertFalse(PlaybackPolicy.isCompleted(18_000, reel))
    }

    @Test
    fun a_reel_with_no_duration_is_never_complete() {
        // Duration is populated by the probe step; a reel that reached the feed
        // without one is a pipeline bug, and reporting 100% completion for it
        // would feed the ranker a reel that always "finishes".
        assertFalse(PlaybackPolicy.isCompleted(10_000, item(durationMs = 0)))
    }
}
