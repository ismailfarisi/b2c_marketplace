package ae.marketplace.mobile.playback

import ae.marketplace.mobile.PlatformContext

/**
 * The entire platform surface of video playback.
 *
 * Seven methods. That is the point of the whole architecture and it is worth
 * being explicit about what is *deliberately absent*: no frame callbacks, no
 * surface handle, no codec configuration, no buffer tuning. Those belong to the
 * platform and exposing them into common code would mean the shared layer
 * quietly acquiring opinions about decoding that it cannot hold correctly for
 * both platforms.
 *
 * If this interface ever needs an eighth method, that is worth a conversation
 * rather than a commit — it is the signal that the split is being eroded.
 */
interface ReelPlayer {
    /** Loads a source without starting playback. Idempotent for the same url. */
    fun prepare(url: String)

    fun play()
    fun pause()

    /** Position in milliseconds, or 0 before anything is prepared. */
    fun positionMs(): Long

    /** True once the player has enough buffered to start without stalling. */
    fun isReady(): Boolean

    /** Releases the decoder. The instance is not reusable afterwards. */
    fun release()

    /** The url currently prepared, for the pool to decide whether to reuse. */
    fun currentUrl(): String?
}

expect fun createReelPlayer(context: PlatformContext): ReelPlayer

/**
 * A fixed set of players, reused across the feed.
 *
 * Not one player per reel. On Android a hardware decoder is a genuinely scarce
 * resource — a mid-range device has a handful, and exhausting them does not
 * degrade, it throws — and on iOS an unbounded set of AVPlayers is the classic
 * way to get a feed killed for memory. A pool the size of the prefetch window
 * plus the visible reel is exactly as many as the policy can ever need at once.
 *
 * The pool is common code because the *sizing rule* is common. What it hands
 * out is platform.
 */
class ReelPlayerPool(
    private val factory: () -> ReelPlayer,
    private val capacity: Int = PlaybackPolicy.PREFETCH_AHEAD + PlaybackPolicy.KEEP_BEHIND + 1,
) {
    private val players = mutableListOf<ReelPlayer>()
    private val assigned = mutableMapOf<String, ReelPlayer>()

    /**
     * Returns the player holding [reelId], creating or evicting as needed.
     *
     * Eviction is least-recently-assigned rather than least-recently-played:
     * the caller assigns in window order, so the entry at the front of the map
     * is by construction the one furthest from the viewer.
     */
    fun acquire(reelId: String): ReelPlayer {
        assigned[reelId]?.let { return it }

        val player = if (players.size < capacity) {
            factory().also { players += it }
        } else {
            val evictedId = assigned.keys.first()
            assigned.remove(evictedId)!!.also { it.pause() }
        }

        assigned[reelId] = player
        return player
    }

    fun assignedTo(reelId: String): ReelPlayer? = assigned[reelId]

    fun size(): Int = players.size

    fun releaseAll() {
        players.forEach { it.release() }
        players.clear()
        assigned.clear()
    }
}
