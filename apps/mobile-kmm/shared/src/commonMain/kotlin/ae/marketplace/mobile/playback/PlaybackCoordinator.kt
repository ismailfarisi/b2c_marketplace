package ae.marketplace.mobile.playback

import ae.marketplace.mobile.events.EventQueue
import ae.marketplace.mobile.model.ClientEvent
import ae.marketplace.mobile.model.FeedItem

/**
 * Decides which reel plays, which are warmed, and what gets reported.
 *
 * All of it common. The platform contributes a [ReelPlayer] and a clock and
 * nothing else, which means the two behaviours that actually cost money —
 * how much video is fetched, and whether an impression is honestly described —
 * have exactly one implementation and a test suite.
 *
 * Sequencing inside [onVisible] matters and is not arbitrary:
 *
 *  1. close out the reel being left, *then*
 *  2. start the new one, *then*
 *  3. warm the window.
 *
 * Closing first is what stops a fast scroll losing watch time — the outgoing
 * reel's position is read before its player can be recycled for the incoming
 * one. Warming last is what stops a prefetch competing for bandwidth with the
 * video the viewer is actually looking at.
 */
class PlaybackCoordinator(
    private val pool: ReelPlayerPool,
    private val events: EventQueue,
    private val metered: () -> Boolean,
    private val now: () -> Long,
) {
    private var currentIndex: Int = -1
    private var currentItem: FeedItem? = null
    private var startedAt: Long = 0

    /** The reel the viewer is looking at. */
    val visibleIndex: Int get() = currentIndex

    /**
     * The player currently holding this reel, if any.
     *
     * The one thing the UI layer needs from here, and it is deliberately
     * read-only and nullable: a surface asks which player to attach to and gets
     * `null` when the reel is outside the window, which is the correct answer —
     * a reel with no player should render its poster, not create a decoder.
     */
    fun playerFor(reelId: String): ReelPlayer? = pool.assignedTo(reelId)

    fun onVisible(index: Int, items: List<FeedItem>) {
        if (items.isEmpty() || index !in items.indices) return
        if (index == currentIndex) return

        closeCurrent()

        val item = items[index]
        currentIndex = index
        currentItem = item
        startedAt = now()

        val player = pool.acquire(item.id)
        prepareIfNeeded(player, item)
        player.play()

        events.enqueue(
            ClientEvent(
                type = ClientEvent.VIEW_START,
                reelId = item.id,
                occurredAt = events.timestamp(),
                properties = exploreProperty(item),
            )
        )

        warmWindow(index, items)
    }

    /**
     * Prepares the reels around the visible one.
     *
     * The window is [PlaybackPolicy.window]'s, minus the visible reel which is
     * already prepared, and each is *prepared and not played*. A prepared player
     * fetches the first segments and stops; a playing one keeps going, which
     * would mean paying for three reels of bandwidth to show one.
     */
    private fun warmWindow(index: Int, items: List<FeedItem>) {
        PlaybackPolicy.window(index, items.size)
            .drop(1)
            .forEach { i ->
                val neighbour = items[i]
                prepareIfNeeded(pool.acquire(neighbour.id), neighbour)
            }
    }

    private fun prepareIfNeeded(player: ReelPlayer, item: FeedItem) {
        val rendition = PlaybackPolicy.select(item.renditions, metered()) ?: return
        if (player.currentUrl() != rendition.playlistUrl) {
            player.prepare(rendition.playlistUrl)
        }
    }

    /**
     * Reports what happened to the reel being left.
     *
     * The watched time is wall-clock since it became visible, not the player's
     * position: a reel that stalled for three seconds was *watched* for three
     * seconds of the viewer's life and zero of the video's, and the ranker
     * should see the number that reflects attention. The player's position is
     * what decides completion, because that one is about the content.
     */
    private fun closeCurrent() {
        val item = currentItem ?: return
        val player = pool.assignedTo(item.id)
        player?.pause()

        val watchedMs = (now() - startedAt).toInt().coerceAtLeast(0)
        val positionMs = player?.positionMs()?.toInt() ?: watchedMs

        events.enqueue(
            ClientEvent(
                // A skip is not a failed view — Phase 4 weights it at −1.5 and
                // needs it. Reporting everything as a view would delete the
                // strongest negative signal the ranker has.
                type = if (PlaybackPolicy.isView(watchedMs)) {
                    ClientEvent.VIEW_PROGRESS
                } else {
                    ClientEvent.SKIP
                },
                reelId = item.id,
                occurredAt = events.timestamp(),
                watchedMs = watchedMs,
                completed = PlaybackPolicy.isCompleted(positionMs, item),
                properties = exploreProperty(item),
            )
        )

        currentItem = null
        currentIndex = -1
    }

    /** Pauses without reporting. For a phone call or a backgrounded app. */
    fun onPause() {
        currentItem?.let { pool.assignedTo(it.id)?.pause() }
    }

    fun onResume() {
        currentItem?.let { pool.assignedTo(it.id)?.play() }
    }

    /** Flushes the current reel's watch time. Called when the feed is left. */
    fun onLeave() {
        closeCurrent()
    }

    fun release() {
        closeCurrent()
        pool.releaseAll()
    }

    /**
     * Carries the exploration flag onto the event.
     *
     * Without it, an impression served because a reel earned it and one served
     * because a slot was reserved for it look identical downstream — and then
     * exploration's cost is unmeasurable, which is exactly how it gets deleted
     * in the first cost-cutting exercise.
     */
    private fun exploreProperty(item: FeedItem): Map<String, String> =
        if (item.explore) mapOf("explore" to "true") else emptyMap()
}
