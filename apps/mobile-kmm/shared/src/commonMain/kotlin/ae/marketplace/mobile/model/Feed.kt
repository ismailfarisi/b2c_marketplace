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
