package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/*
 * Hand-written from api/openapi.yaml.
 *
 * These are a *copy* of the spec's schemas, which means they can drift, and the
 * mitigation is that they are one file and short. When `make gen-kotlin` exists
 * (see apps/mobile-kmm/README.md §4) this file is deleted rather than migrated.
 *
 * The web client made the same trade in apps/web/src/lib/api.ts. What is
 * different here is the consequence: with mobile on Kotlin, `api/openapi.yaml`
 * is the *only* artefact both clients share, so drift is no longer caught by a
 * shared TypeScript type — it is caught by a runtime deserialisation failure on
 * whichever platform noticed first.
 */

@Serializable
data class FeedPage(
    val items: List<FeedItem> = emptyList(),
    /**
     * Opaque, and never parsed. The API says so and means it: the format
     * already changed once, between Phase 2 and Phase 4, and it now carries the
     * exclusion set rather than a score.
     */
    @SerialName("next_cursor") val nextCursor: String? = null,
    /** Which weight set produced this page. Logged, never branched on. */
    @SerialName("ranking_profile") val rankingProfile: String? = null,
    /**
     * Which surface actually produced this page.
     *
     * Echoed rather than assumed, because the server defaults a surface it does
     * not recognise. A client that highlighted the tab it *asked* for would
     * label a For you page as Live now on the first build that outlived a tab.
     */
    val surface: String? = null,
    /**
     * Why [items] is empty, or null when it is not. See FeedEmptyReason — the
     * client cannot derive this, because "no reels" and "no reels that are live"
     * are the same empty array.
     */
    @SerialName("empty_reason") val emptyReason: String? = null,
)

@Serializable
data class FeedItem(
    val id: String,
    val author: Author,
    @SerialName("subject_type") val subjectType: String? = null,
    @SerialName("subject_id") val subjectId: String? = null,
    @SerialName("category_id") val categoryId: String? = null,
    val caption: String = "",
    @SerialName("duration_ms") val durationMs: Int = 0,
    @SerialName("poster_key") val posterKey: String = "",
    @SerialName("preview_key") val previewKey: String = "",
    /**
     * BlurHash or a base64 micro-thumb, inline. The feed paints this before any
     * network fetch resolves, which is the difference between a scroll that
     * feels instant and one that flashes black between reels.
     */
    val lqip: String = "",
    val renditions: List<Rendition> = emptyList(),
    val counters: Counters = Counters(),
    @SerialName("published_at") val publishedAt: String = "",
    /**
     * This slot was reserved for exploration rather than earned on score.
     *
     * Carried into the impression event so exploration's cost stays measurable.
     * Without the distinction the first cost-cutting exercise deletes
     * exploration, and then new vendors never get impressions.
     */
    val explore: Boolean = false,
    val viewer: ViewerState = ViewerState(),
    val actions: List<ResolvedAction> = emptyList(),
    /**
     * When the thing this reel is about stops being true. Null for a reel with
     * no time dimension, which is a normal state and not a degraded one.
     */
    @SerialName("time_signal") val timeSignal: TimeSignal? = null,
    /** The badge and the lines above the CTA. Null when the subject has no provider. */
    val summary: SubjectSummary? = null,
)

/**
 * The time bar and the pill beside it.
 *
 * Note what is absent: any rendered string. The server sends instants and this
 * client turns them into words, because a server-rendered "02:04 left" is stale
 * by the time it is parsed, and because the same page is served to an English
 * and an Arabic viewer whose digits differ.
 *
 * Rendering is driven by [mode] alone — the same discipline as [ResolvedAction],
 * and for the same reason. There is deliberately no `when (subjectType)` in the
 * reel overlay: the moment one exists, adding a vertical means shipping a
 * mobile release.
 *
 * Timestamps are ISO-8601 strings rather than `Instant` because this file is a
 * hand-written mirror of the wire format and parsing belongs one layer up, where
 * a malformed timestamp can degrade the overlay instead of failing the page.
 */
@Serializable
data class TimeSignal(
    /** `countdown` | `window` | `freshness` | `cutoff`. Unknown values render nothing. */
    val mode: String,
    @SerialName("starts_at") val startsAt: String? = null,
    @SerialName("ends_at") val endsAt: String? = null,
    /** The bar's denominator when the interval itself doesn't give a useful one. */
    @SerialName("horizon_seconds") val horizonSeconds: Int? = null,
    /** IANA name, present for `cutoff`. Format the wall time in *this* zone, not the device's. */
    val timezone: String? = null,
    /**
     * The boundary comes back.
     *
     * Decides what happens when the clock hits zero: a countdown that expires
     * drops the reel from the pager, a cutoff that passes rolls to the next
     * cycle and the CTA stays live.
     */
    val recurring: Boolean = false,
    /**
     * The server's clock when the page was built. **Apply it.**
     *
     * `skew = asOf - deviceNow`, computed once per page and added to every
     * countdown on it. A device four minutes fast renders a live auction as
     * already closed, and the user concludes the app is broken rather than that
     * their clock is.
     *
     * Carried per signal rather than per page even though every signal on a page
     * holds the same value — a signal handed to a detail screen has to stay
     * correct after it leaves the page it came from.
     */
    @SerialName("as_of") val asOf: String,
)

/**
 * The overlay's text, composed entirely by the owning vertical.
 *
 * Arabic variants arrive alongside rather than being selected server-side: the
 * client already knows the active locale, and a page cached under one locale and
 * rendered under another is a real sequence on a device that changes language
 * without a cold start.
 */
@Serializable
data class SubjectSummary(
    /** "USED CAR · VERIFIED" — the kind of thing, read before the viewer decides to scroll. */
    val badge: String = "",
    @SerialName("badge_ar") val badgeAr: String = "",
    val title: String = "",
    @SerialName("title_ar") val titleAr: String = "",
    /** Pre-joined by the vertical. Do not split and re-join: the separator is wrong in RTL. */
    val line: String = "",
    @SerialName("line_ar") val lineAr: String = "",
    /** Minor units. Null currency means no price to show, which is not the same as free. */
    @SerialName("price_minor") val priceMinor: Long? = null,
    val currency: String? = null,
    /** "/wk", "/mo". Apart from the number so it can render smaller. */
    @SerialName("price_suffix") val priceSuffix: String = "",
    @SerialName("price_suffix_ar") val priceSuffixAr: String = "",
)

@Serializable
data class Author(
    val type: String,
    val id: String,
    val name: String = "",
)

@Serializable
data class Rendition(
    /** "360p", "540p", "720p". Capped at 720p server-side — see PlaybackPolicy. */
    val label: String,
    val width: Int,
    val height: Int,
    @SerialName("bitrate_kbps") val bitrateKbps: Int,
    @SerialName("playlist_url") val playlistUrl: String,
)

/**
 * Read from the hot store with a Postgres fallback, so they lag by up to the
 * flush interval. Never treated as transactionally consistent — the UI
 * increments optimistically on a like and lets the next page correct it.
 */
@Serializable
data class Counters(
    val views: Long = 0,
    val likes: Long = 0,
    val comments: Long = 0,
    val shares: Long = 0,
    val saves: Long = 0,
)

@Serializable
data class ViewerState(
    val liked: Boolean = false,
    val saved: Boolean = false,
    val followed: Boolean = false,
)

/**
 * A CTA as the client renders it.
 *
 * `label`, `available` and `reason` come from the owning vertical's handler; the
 * feed never learns what the target is, and neither does this client. Rendering
 * is driven entirely by [kind] and [available] — there is deliberately no
 * `when (targetType)` anywhere in the app, because the moment one exists, adding
 * a vertical means shipping a mobile release.
 */
@Serializable
data class ResolvedAction(
    val id: String,
    val kind: String,
    @SerialName("target_type") val targetType: String,
    @SerialName("target_id") val targetId: String? = null,
    val position: Int = 0,
    val label: String = "",
    @SerialName("label_ar") val labelAr: String = "",
    val available: Boolean = true,
    /**
     * Always populated when unavailable. A greyed-out button with no
     * explanation reads as a broken app, and the vendor gets blamed for a
     * platform decision.
     */
    val reason: String? = null,
    val route: String = "",
)
