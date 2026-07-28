package ae.marketplace.mobile.events

import ae.marketplace.mobile.model.ClientEvent
import ae.marketplace.mobile.model.EventBatch
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Batches engagement events and posts them.
 *
 * The API is explicit about why this exists: *"one request per event would turn
 * a 30-second viewing session into a dozen requests per user"*. What the API
 * cannot enforce is the client half of the contract, which is this file.
 *
 * Three rules, each with a failure it prevents:
 *
 *  - **Flush on an interval, on a size threshold, and on background.** Interval
 *    alone loses the last batch of every session, which is the batch containing
 *    the reel someone actually converted on.
 *  - **Never drop the newest.** When the buffer overflows the *oldest* events
 *    go, because the newest ones are the ones nearest a conversion.
 *  - **Keep the batch on failure, re-send whole.** Ingest is documented as
 *    idempotent, so re-sending is safe; dropping is not, and a client that drops
 *    on a flaky network under-reports precisely on the connections where
 *    engagement is already worst.
 */
class EventQueue(
    private val sender: EventSender,
    private val sessionId: String,
    private val timestampProvider: () -> String,
    private val scope: CoroutineScope,
    private val flushIntervalMs: Long = FLUSH_INTERVAL_MS,
) {
    companion object {
        /** Roughly every ten seconds, per the API description. */
        const val FLUSH_INTERVAL_MS = 10_000L

        /** The server's documented cap. Batches are never built larger. */
        const val MAX_BATCH = 200

        /**
         * How many events are held before the oldest are dropped.
         *
         * Ten batches. A viewer on a plane produces events for as long as they
         * scroll, and the alternative to a bound is an app that grows until it
         * is killed — losing everything, including the recent events a bound
         * would have kept.
         */
        const val MAX_BUFFER = MAX_BATCH * 10
    }

    private val mutex = Mutex()
    private val buffer = ArrayDeque<ClientEvent>()
    private var dropped = 0L
    private var ticker: Job? = null

    fun timestamp(): String = timestampProvider()

    /**
     * Adds an event. Never suspends and never does I/O.
     *
     * Called from the playback path on every swipe, so anything slower than a
     * lock acquisition here shows up as scroll jank — which is the one thing a
     * video feed cannot have.
     */
    fun enqueue(event: ClientEvent) {
        scope.launch {
            mutex.withLock {
                buffer.addLast(event)
                while (buffer.size > MAX_BUFFER) {
                    buffer.removeFirst()
                    dropped++
                }
            }
        }
    }

    fun start() {
        if (ticker != null) return
        ticker = scope.launch {
            while (isActive) {
                delay(flushIntervalMs)
                flush()
            }
        }
    }

    fun stop() {
        ticker?.cancel()
        ticker = null
    }

    /**
     * Sends everything buffered, in batches.
     *
     * Returns the number of events accepted. On failure the un-sent remainder is
     * put *back at the front* of the buffer rather than appended, so ordering
     * survives a retry — the ranker reads `occurred_at`, but a batch that
     * arrives with its events shuffled makes every debugging session harder for
     * no gain.
     */
    suspend fun flush(): Int {
        var sent = 0
        while (true) {
            val batch = mutex.withLock {
                if (buffer.isEmpty()) return sent
                val take = minOf(MAX_BATCH, buffer.size)
                List(take) { buffer.removeFirst() }
            }

            val ok = runCatching { sender.send(EventBatch(sessionId, batch)) }.isSuccess
            if (!ok) {
                mutex.withLock { batch.asReversed().forEach { buffer.addFirst(it) } }
                return sent
            }
            sent += batch.size
        }
    }

    /**
     * How many events were discarded because the buffer was full.
     *
     * Exposed rather than logged and forgotten. A sink that loses events
     * silently is a conversion rate that is wrong silently — the same reason the
     * Go ClickHouse sink counts its drops.
     */
    suspend fun droppedCount(): Long = mutex.withLock { dropped }

    suspend fun pending(): Int = mutex.withLock { buffer.size }
}

fun interface EventSender {
    suspend fun send(batch: EventBatch)
}
