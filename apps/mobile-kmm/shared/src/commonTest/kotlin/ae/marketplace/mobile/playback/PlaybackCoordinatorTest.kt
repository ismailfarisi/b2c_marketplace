package ae.marketplace.mobile.playback

import ae.marketplace.mobile.events.EventQueue
import ae.marketplace.mobile.events.EventSender
import ae.marketplace.mobile.model.Author
import ae.marketplace.mobile.model.ClientEvent
import ae.marketplace.mobile.model.EventBatch
import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.model.Rendition
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * A fake player, so the coordinator's decisions are testable without a decoder.
 *
 * Being able to write this at all is the argument for the seven-method
 * [ReelPlayer] interface: a wider one would need a fake with opinions about
 * buffering, and then the test would be testing the fake.
 */
private class FakePlayer : ReelPlayer {
    var prepared: String? = null
    var prepareCount = 0
    var playing = false
    var released = false
    var position = 0L

    override fun prepare(url: String) {
        if (prepared != url) prepareCount++
        prepared = url
    }

    override fun play() { playing = true }
    override fun pause() { playing = false }
    override fun positionMs(): Long = position
    override fun isReady(): Boolean = prepared != null
    override fun release() { released = true }
    override fun currentUrl(): String? = prepared
}

@OptIn(ExperimentalCoroutinesApi::class)
class PlaybackCoordinatorTest {

    private fun feed(n: Int) = List(n) { i ->
        FeedItem(
            id = "reel-$i",
            author = Author(type = "vendor_branch", id = "b1"),
            durationMs = 15_000,
            explore = i == 3,
            renditions = listOf(
                Rendition("360p", 640, 360, 600, "https://cdn/$i-360.m3u8"),
                Rendition("720p", 1280, 720, 2200, "https://cdn/$i-720.m3u8"),
            ),
        )
    }

    private class Harness(scope: TestScope, val metered: Boolean = false) {
        val sent = mutableListOf<ClientEvent>()
        val players = mutableListOf<FakePlayer>()
        var clock = 0L

        val queue = EventQueue(
            sender = EventSender { batch: EventBatch -> sent += batch.events },
            sessionId = "s1",
            timestampProvider = { "2026-07-28T12:00:00Z" },
            scope = scope,
        )

        val coordinator = PlaybackCoordinator(
            pool = ReelPlayerPool(factory = { FakePlayer().also { players += it } }),
            events = queue,
            metered = { metered },
            now = { clock },
        )
    }

    @Test
    fun warms_exactly_the_window_and_plays_only_the_visible_reel() = runTest {
        // The whole egress argument in one assertion. Three reels prepared, one
        // playing: a prepared player fetches the first segments and stops, while
        // a playing one keeps going.
        val h = Harness(this)
        val items = feed(10)

        h.coordinator.onVisible(0, items)

        val prepared = h.players.count { it.prepared != null }
        assertEquals(3, prepared, "expected the visible reel plus two ahead")
        assertEquals(1, h.players.count { it.playing }, "only the visible reel plays")
    }

    @Test
    fun the_pool_is_bounded_however_far_you_scroll() = runTest {
        // Hardware decoders are genuinely scarce on Android — exhausting them
        // throws rather than degrades.
        val h = Harness(this)
        val items = feed(60)

        for (i in items.indices) h.coordinator.onVisible(i, items)

        val capacity = PlaybackPolicy.PREFETCH_AHEAD + PlaybackPolicy.KEEP_BEHIND + 1
        assertTrue(
            h.players.size <= capacity,
            "created ${h.players.size} players scrolling 60 reels; cap is $capacity",
        )
    }

    @Test
    fun a_short_look_is_reported_as_a_skip() = runTest {
        // Phase 4 weights `skip` at −1.5 and needs it. A client that called
        // everything a view would delete the strongest negative signal the
        // ranker has.
        val h = Harness(this)
        val items = feed(5)

        h.coordinator.onVisible(0, items)
        h.clock += 800
        h.coordinator.onVisible(1, items)
        testScheduler.advanceUntilIdle()
        h.queue.flush()

        val closing = h.sent.first { it.reelId == "reel-0" && it.type != ClientEvent.VIEW_START }
        assertEquals(ClientEvent.SKIP, closing.type)
        assertEquals(800, closing.watchedMs)
    }

    @Test
    fun a_real_watch_is_reported_as_progress() = runTest {
        val h = Harness(this)
        val items = feed(5)

        h.coordinator.onVisible(0, items)
        h.clock += 6_000
        h.coordinator.onVisible(1, items)
        testScheduler.advanceUntilIdle()
        h.queue.flush()

        val closing = h.sent.first { it.reelId == "reel-0" && it.type != ClientEvent.VIEW_START }
        assertEquals(ClientEvent.VIEW_PROGRESS, closing.type)
        assertEquals(6_000, closing.watchedMs)
    }

    @Test
    fun watch_time_survives_a_fast_scroll() = runTest {
        // The outgoing reel is closed out before its player can be recycled for
        // the incoming one. Get the ordering wrong and fast scrolling silently
        // loses watch time on exactly the reels people flick past — which is
        // most of them.
        val h = Harness(this)
        val items = feed(20)

        for (i in 0 until 12) {
            h.coordinator.onVisible(i, items)
            h.clock += 500
        }
        h.coordinator.onLeave()
        testScheduler.advanceUntilIdle()
        h.queue.flush()

        val closings = h.sent.filter { it.type == ClientEvent.SKIP || it.type == ClientEvent.VIEW_PROGRESS }
        assertEquals(12, closings.size, "every reel visited should report exactly once")
        assertTrue(closings.all { (it.watchedMs ?: 0) > 0 }, "no reel should report zero watch time")
    }

    @Test
    fun completion_uses_the_player_position_not_wall_clock() = runTest {
        // A viewer who leaves the app open on a reel for a minute has not
        // watched it four times. Wall clock measures attention; the player's
        // position measures the content.
        val h = Harness(this)
        val items = feed(3)

        h.coordinator.onVisible(0, items)
        h.players.first().position = 3_000
        h.clock += 60_000
        h.coordinator.onVisible(1, items)
        testScheduler.advanceUntilIdle()
        h.queue.flush()

        val closing = h.sent.first { it.reelId == "reel-0" && it.type == ClientEvent.VIEW_PROGRESS }
        assertFalse(closing.completed, "3s of a 15s reel is not a completion")
        assertEquals(60_000, closing.watchedMs)
    }

    @Test
    fun the_exploration_flag_rides_along() = runTest {
        // Without it, an impression a reel earned and one served from a reserved
        // slot look identical downstream — and then exploration's cost is
        // unmeasurable, which is how it gets cut.
        val h = Harness(this)
        val items = feed(6)

        h.coordinator.onVisible(3, items)
        h.clock += 3_000
        h.coordinator.onVisible(4, items)
        testScheduler.advanceUntilIdle()
        h.queue.flush()

        val explored = h.sent.filter { it.reelId == "reel-3" }
        assertTrue(explored.isNotEmpty())
        assertTrue(explored.all { it.properties["explore"] == "true" })
    }

    @Test
    fun re_reporting_the_same_index_is_a_no_op() = runTest {
        // A pager that re-emits the visible index on recomposition must not
        // produce a second view_start, or every configuration change inflates
        // the view count.
        val h = Harness(this)
        val items = feed(5)

        h.coordinator.onVisible(2, items)
        h.coordinator.onVisible(2, items)
        h.coordinator.onVisible(2, items)
        testScheduler.advanceUntilIdle()
        h.queue.flush()

        assertEquals(1, h.sent.count { it.type == ClientEvent.VIEW_START })
    }

    @Test
    fun a_metered_connection_prepares_the_smaller_rendition() = runTest {
        val h = Harness(this, metered = true)
        h.coordinator.onVisible(0, feed(3))
        assertTrue(h.players.all { it.prepared == null || it.prepared!!.contains("-360") })
    }

    @Test
    fun scrolling_back_does_not_re_prepare() = runTest {
        // The one-behind rule, observed rather than asserted on the constant.
        val h = Harness(this)
        val items = feed(10)

        h.coordinator.onVisible(0, items)
        h.coordinator.onVisible(1, items)
        val reprepares = h.players.sumOf { it.prepareCount }

        h.coordinator.onVisible(0, items)
        assertEquals(reprepares, h.players.sumOf { it.prepareCount },
            "reel 0 was still warm and should not have been prepared again")
    }
}
