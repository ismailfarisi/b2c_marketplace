package ae.marketplace.mobile.events

import ae.marketplace.mobile.model.ClientEvent
import ae.marketplace.mobile.model.EventBatch
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class EventQueueTest {

    private fun event(i: Int) = ClientEvent(
        type = ClientEvent.VIEW_PROGRESS,
        reelId = "reel-$i",
        occurredAt = "2026-07-28T12:00:00Z",
        watchedMs = 3_000,
    )

    @Test
    fun batches_rather_than_sending_one_request_per_event() = runTest {
        val batches = mutableListOf<EventBatch>()
        val q = EventQueue(EventSender { batches += it }, "s1", { "t" }, this)

        repeat(30) { q.enqueue(event(it)) }
        testScheduler.advanceUntilIdle()
        q.flush()

        assertEquals(1, batches.size, "30 events should be one request, not 30")
        assertEquals(30, batches.first().events.size)
    }

    @Test
    fun never_exceeds_the_servers_batch_cap() = runTest {
        val batches = mutableListOf<EventBatch>()
        val q = EventQueue(EventSender { batches += it }, "s1", { "t" }, this)

        repeat(450) { q.enqueue(event(it)) }
        testScheduler.advanceUntilIdle()
        q.flush()

        assertTrue(batches.all { it.events.size <= EventQueue.MAX_BATCH })
        assertEquals(450, batches.sumOf { it.events.size }, "nothing lost across batches")
    }

    @Test
    fun a_failed_flush_keeps_the_events() = runTest {
        // Ingest is documented as idempotent, so re-sending is safe and dropping
        // is not. A client that dropped on a flaky network would under-report on
        // exactly the connections where engagement is already worst.
        var fail = true
        val q = EventQueue(
            EventSender { if (fail) error("network") },
            "s1", { "t" }, this,
        )

        repeat(10) { q.enqueue(event(it)) }
        testScheduler.advanceUntilIdle()

        assertEquals(0, q.flush())
        assertEquals(10, q.pending(), "a failed send must not lose events")

        fail = false
        assertEquals(10, q.flush())
        assertEquals(0, q.pending())
    }

    @Test
    fun a_failed_flush_preserves_order() = runTest {
        var fail = true
        val seen = mutableListOf<ClientEvent>()
        val q = EventQueue(
            EventSender { if (fail) error("network") else seen += it.events },
            "s1", { "t" }, this,
        )

        repeat(5) { q.enqueue(event(it)) }
        testScheduler.advanceUntilIdle()
        q.flush()

        fail = false
        q.flush()

        assertEquals(listOf("reel-0", "reel-1", "reel-2", "reel-3", "reel-4"), seen.map { it.reelId })
    }

    @Test
    fun overflow_drops_the_oldest_and_counts_it() = runTest {
        // The newest events are the ones nearest a conversion. And a sink that
        // loses events silently is a conversion rate that is wrong silently —
        // the same reason the Go ClickHouse sink counts its drops.
        val q = EventQueue(EventSender { }, "s1", { "t" }, this)

        repeat(EventQueue.MAX_BUFFER + 250) { q.enqueue(event(it)) }
        testScheduler.advanceUntilIdle()

        assertEquals(EventQueue.MAX_BUFFER, q.pending())
        assertEquals(250L, q.droppedCount())
    }

    @Test
    fun overflow_keeps_the_newest() = runTest {
        val seen = mutableListOf<ClientEvent>()
        val q = EventQueue(EventSender { seen += it.events }, "s1", { "t" }, this)

        repeat(EventQueue.MAX_BUFFER + 5) { q.enqueue(event(it)) }
        testScheduler.advanceUntilIdle()
        q.flush()

        assertEquals("reel-${EventQueue.MAX_BUFFER + 4}", seen.last().reelId)
    }

    @Test
    fun enqueue_does_not_block_the_caller() = runTest {
        // Called from the swipe path. Anything slower than a lock acquisition
        // here shows up as scroll jank, which is the one thing a video feed
        // cannot have.
        val q = EventQueue(EventSender { kotlinx.coroutines.delay(10_000) }, "s1", { "t" }, this)
        repeat(100) { q.enqueue(event(it)) }
        // Returns without the sender having run at all.
        assertEquals(0, q.pending())
    }

    @Test
    fun flushing_an_empty_queue_sends_nothing() = runTest {
        var calls = 0
        val q = EventQueue(EventSender { calls++ }, "s1", { "t" }, this)
        assertEquals(0, q.flush())
        assertEquals(0, calls, "an idle app must not wake the radio every ten seconds")
    }
}
