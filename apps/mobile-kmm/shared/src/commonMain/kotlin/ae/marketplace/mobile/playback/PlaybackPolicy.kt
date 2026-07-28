package ae.marketplace.mobile.playback

import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.model.Rendition

/**
 * What to load, how much of it, and how far ahead.
 *
 * **This file is the reason the app is Kotlin Multiplatform.** The decoder is
 * platform code — ExoPlayer on Android, AVPlayer on iOS — and it should be,
 * because that is where the performance is. But *the policy around the decoder*
 * is identical on both platforms and is directly attached to a business risk:
 *
 * > PLAN-VIDEO-FIRST §9, second-ranked risk: "Cost per view with no conversion —
 * > ~$0.11–0.17/GB egress in the region; you pay for every view, converting or
 * > not. Mitigation: preload ≤2 ahead, cap 720p."
 *
 * Two implementations of that mitigation drift, and when they drift the symptom
 * is a cloud bill, six weeks later, that nobody can attribute to a client
 * change. One implementation with tests is the whole argument.
 */
object PlaybackPolicy {

    /**
     * How many reels ahead of the visible one are prepared.
     *
     * Two, from the plan. It is a direct multiplier on egress: at three, a
     * viewer who scrolls fast enough pays for a third of the video they never
     * see. At one, the next reel visibly buffers, which is the thing that makes
     * a feed feel broken. Two is the plan's number and it is also the smallest
     * number that hides a swipe.
     */
    const val PREFETCH_AHEAD = 2

    /**
     * One behind, kept warm.
     *
     * Scrolling back up is common — someone re-reads a caption or reconsiders a
     * CTA — and re-preparing a reel they were watching four seconds ago is the
     * most obviously wasteful buffer in the product.
     */
    const val KEEP_BEHIND = 1

    /** Ceiling label. The server caps at 720p; the client must not out-request it. */
    const val MAX_LABEL = "720p"

    /**
     * On a metered connection, drop a step.
     *
     * Not to save the platform money — the egress is already paid at this point —
     * but to save the *viewer's*. A GCC prepaid data plan is a real constraint
     * and an app that burns it is an app that gets deleted.
     */
    const val METERED_LABEL = "540p"

    /** A view is ≥2s watched. Applied by the server; used here to decide what is worth reporting. */
    const val VIEW_THRESHOLD_MS = 2_000

    private val ladder = listOf("360p", "540p", "720p")

    /**
     * Picks the rendition to play.
     *
     * Deliberately not "the highest available". The rules, in order:
     *
     *  1. never above [MAX_LABEL] — 1080p roughly doubles egress for a
     *     difference nobody sees on a phone, which is why the server does not
     *     produce it either;
     *  2. never above [METERED_LABEL] on a metered connection;
     *  3. otherwise the highest rendition at or below the ceiling.
     *
     * Falls back to the *lowest* available when nothing fits, not the highest: a
     * viewer on a bad connection is better served by a reel that starts than by
     * one that buffers.
     */
    fun select(
        renditions: List<Rendition>,
        metered: Boolean,
        maxLabel: String = MAX_LABEL,
    ): Rendition? {
        if (renditions.isEmpty()) return null

        var ceiling = rank(maxLabel)
        if (metered) ceiling = minOf(ceiling, rank(METERED_LABEL))

        return renditions
            .filter { rank(it.label) <= ceiling }
            .maxByOrNull { rank(it.label) }
            ?: renditions.minByOrNull { rank(it.label) }
    }

    /**
     * An unrecognised label ranks *above* the ladder, so it is filtered out
     * rather than played.
     *
     * The safe direction for a rendition this client has never heard of is not
     * to play it. If the server starts producing 1080p, an old build should keep
     * requesting 720p rather than opting itself into double the egress.
     */
    private fun rank(label: String): Int =
        ladder.indexOf(label).let { if (it < 0) ladder.size else it }

    /**
     * Which indices should have a prepared player, given the visible one.
     *
     * Returned as an ordered list rather than a set: the visible reel is first
     * so a caller that can only prepare one thing prepares the right thing, and
     * the window is clamped to the list so the tail of the feed does not
     * request pages that do not exist yet.
     */
    fun window(current: Int, size: Int): List<Int> {
        if (size <= 0) return emptyList()
        val clamped = current.coerceIn(0, size - 1)
        val ordered = mutableListOf(clamped)
        for (i in 1..PREFETCH_AHEAD) {
            val ahead = clamped + i
            if (ahead < size) ordered += ahead
        }
        for (i in 1..KEEP_BEHIND) {
            val behind = clamped - i
            if (behind >= 0) ordered += behind
        }
        return ordered
    }

    /**
     * Whether a reel is worth reporting a view for.
     *
     * The event is still *sent* below the threshold — the server needs skips to
     * rank, and Phase 4 weights `skip` at −1.5 — this only decides whether it is
     * described as a view or a skip.
     */
    fun isView(watchedMs: Int): Boolean = watchedMs >= VIEW_THRESHOLD_MS

    /**
     * Whether a reel counts as completed.
     *
     * 95%, not 100%. A trailing keyframe, a rounding difference between the
     * container duration and the decoder's, or a viewer swiping on the last
     * half-second all produce a legitimate completion that never reaches the
     * exact end — and treating those as incomplete understates completion rate
     * on precisely the best-performing content.
     */
    fun isCompleted(watchedMs: Int, item: FeedItem): Boolean {
        if (item.durationMs <= 0) return false
        return watchedMs >= (item.durationMs * 95) / 100
    }
}
