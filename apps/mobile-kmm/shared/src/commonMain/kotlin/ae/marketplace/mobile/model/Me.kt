package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/*
 * The three screens behind the tabs that were stubs: Saved, Me, and the
 * messaging that the reel's CTA has always pointed at and never reached.
 *
 * Hand-written from the API, same caveat as Feed.kt and Sell.kt.
 */

/**
 * One saved reel.
 *
 * Its words come from [SubjectSummary] — the vertical's, resolved through the
 * same SummarizePage the feed uses. A saved car and a saved midnight offer
 * render through one composable, and it knows what neither of them is.
 */
@Serializable
data class SavedItem(
    @SerialName("reel_id") val reelId: String,
    @SerialName("subject_type") val subjectType: String = "",
    @SerialName("subject_id") val subjectId: String? = null,
    /** Absolute, composed server-side. Empty when the reel has no still yet. */
    @SerialName("poster_url") val posterUrl: String = "",
    val lqip: String = "",
    @SerialName("saved_at") val savedAt: String = "",
    /**
     * The reel is no longer in the feed — sold, removed, expired.
     *
     * Shown greyed rather than dropped. A saved car that sold is something the
     * viewer chose to keep, and silently deleting it from their own list reads
     * as the app losing their data.
     */
    val unavailable: Boolean = false,
    val summary: SubjectSummary? = null,
)

@Serializable
data class SavedPage(val items: List<SavedItem> = emptyList())

/**
 * One of the seller's own listings, in whatever state it is actually in.
 *
 * Both states are here — the listing's and its reel's — because they fail
 * independently and the seller's question is almost always "why is my car not
 * showing up". More often than not the answer is the video, not the listing.
 */
@Serializable
data class MyListing(
    val id: String,
    @SerialName("reference_no") val referenceNo: String = "",
    /** draft | pending_review | active | paused | sold | expired | rejected */
    val status: String,
    val title: String = "",
    val year: Int = 0,
    @SerialName("mileage_km") val mileageKm: Int = 0,
    @SerialName("price_minor") val priceMinor: Long = 0,
    val currency: String = "",
    val city: String = "",
    @SerialName("reel_id") val reelId: String? = null,
    /** Empty for a listing with no reel, which is normal for the web form. */
    @SerialName("reel_state") val reelState: String = "",
    /** The reel's still, absolute. Empty while it is still transcoding. */
    @SerialName("poster_url") val posterUrl: String = "",
    val lqip: String = "",
    @SerialName("published_at") val publishedAt: String? = null,
    @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("sold_at") val soldAt: String? = null,
)

@Serializable
data class MyListingsPage(val items: List<MyListing> = emptyList())

// ─────────────────────────────────────────────────────────────────────────────
// Search
// ─────────────────────────────────────────────────────────────────────────────

/**
 * A search result.
 *
 * Note that this is a *listing*, not a reel — search runs against the
 * classifieds index, which is where the structured facts live. The feed is the
 * discovery surface and search is the intent surface, and conflating them would
 * mean either a feed you cannot filter by mileage or a search you cannot rank
 * by watch time.
 */
@Serializable
data class SearchResult(
    val id: String,
    @SerialName("reference_no") val referenceNo: String = "",
    val title: String = "",
    val year: Int = 0,
    @SerialName("mileage_km") val mileageKm: Int = 0,
    @SerialName("price_minor") val priceMinor: Long = 0,
    val currency: String = "",
    @SerialName("regional_spec") val regionalSpec: String = "",
    @SerialName("seller_kind") val sellerKind: String = "",
    val emirate: String = "",
    val city: String = "",
    @SerialName("distance_m") val distanceM: Long? = null,
    /**
     * The card's picture, from the listing's newest published reel.
     *
     * Empty for a listing created through the web form, which has no video.
     * That is permanent rather than pending, so the client draws a flat block
     * rather than waiting.
     */
    @SerialName("poster_url") val posterUrl: String = "",
    val lqip: String = "",
    /**
     * Paid placement, always labelled.
     *
     * A promoted result that looks organic is the thing that makes buyers stop
     * trusting the order — at which point the placement the seller paid for is
     * worth nothing.
     */
    val promoted: Boolean = false,
    /**
     * The listing's published reel, when it has one.
     *
     * Null for a listing created through the web form. The client opens the
     * video when there is one and falls through to a conversation when there is
     * not — going straight to chat for everything skipped the reel entirely, on
     * the one surface where a buyer most wants to see the car before they ask
     * about it.
     */
    @SerialName("reel_id") val reelId: String? = null,
)

@Serializable
data class SearchPage(
    val items: List<SearchResult> = emptyList(),
    val total: Long = 0,
    val limit: Int = 0,
)

// ─────────────────────────────────────────────────────────────────────────────
// Messaging
// ─────────────────────────────────────────────────────────────────────────────

@Serializable
data class Conversation(
    val id: String,
    @SerialName("listing_id") val listingId: String? = null,
    @SerialName("listing_title") val listingTitle: String = "",
    @SerialName("counterparty_id") val counterpartyId: String? = null,
    val unread: Int = 0,
    @SerialName("last_message_at") val lastMessageAt: String? = null,
    val state: String = "",
)

@Serializable
data class Inbox(val items: List<Conversation> = emptyList())

/**
 * One message.
 *
 * [state] is not decoration. A message can be `held` — under review and *not
 * delivered* — or `redacted`, with contact details stripped. Both must be shown
 * as what they are: a sender who believes a held message arrived will conclude
 * they are being ignored and go find a phone number, which is exactly the
 * off-platform move the redaction exists to prevent.
 */
@Serializable
data class Message(
    val id: String,
    val body: String = "",
    /** sent | held | redacted | blocked */
    val state: String = "sent",
    @SerialName("sender_id") val senderId: String? = null,
    /**
     * Whether the viewer wrote it.
     *
     * Computed server-side. The client has no idea which uuid is its own — the
     * token exchange never tells it — so without this every bubble rendered
     * identically and the thread was a transcript with the speaker labels torn
     * off.
     */
    val mine: Boolean = false,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class Thread(val items: List<Message> = emptyList())

/** What POST /listings/{id}/inquire hands back: the thread to open. */
@Serializable
data class Inquiry(
    @SerialName("lead_id") val leadId: String? = null,
    @SerialName("conversation_id") val conversationId: String,
)
