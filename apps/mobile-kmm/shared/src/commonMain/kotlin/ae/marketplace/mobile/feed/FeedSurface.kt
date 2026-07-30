package ae.marketplace.mobile.feed

/**
 * The tab row at the top of the feed.
 *
 * An enum here and a string on the wire, which is the right way round: the
 * client renders a fixed set of tabs it was built with, and the server may know
 * about others. A surface the server retires is served as [ForYou] rather than
 * rejected, so an old build keeps working — the response echoes what it actually
 * got, and [FeedUiState.servedSurface] is what the tab row should highlight.
 *
 * Ordering here is the ordering on screen, and it is not alphabetical or
 * arbitrary. [ForYou] first because it is the default and the one that works
 * with no supply assumptions; the two time surfaces next, because they are the
 * product's actual claim; [Nearby] last because it is the fallback a viewer
 * reaches for when the others are thin.
 */
enum class FeedSurface(val wire: String) {
    ForYou("for_you"),
    LiveNow("live_now"),
    EndingSoon("ending_soon"),
    Nearby("nearby");

    companion object {
        /**
         * Maps the value the server echoed back.
         *
         * Falls back to [ForYou] rather than throwing, matching what the server
         * does with the request. A client that crashed on an unknown surface
         * would turn a forwards-compatible API into a forced upgrade.
         */
        fun fromWire(raw: String?): FeedSurface =
            entries.firstOrNull { it.wire == raw } ?: ForYou
    }
}

/**
 * Why a surface came back empty.
 *
 * Distinct from "loading" and from "error", and the distinction is the whole
 * point: *nothing is live right now* is a true statement about a working
 * product, and rendering it as the generic empty state makes the app look
 * broken. Which of those a viewer believes decides whether they try the next tab
 * or close the app.
 *
 * The client cannot derive this — "no reels" and "no reels that are live" are
 * the same empty array — so it comes from the server.
 */
enum class FeedEmptyReason(val wire: String) {
    NothingLive("nothing_live"),
    NothingClosing("nothing_closing"),
    NothingNear("nothing_near"),
    NothingEligible("nothing_eligible");

    companion object {
        fun fromWire(raw: String?): FeedEmptyReason? =
            entries.firstOrNull { it.wire == raw }
    }
}
