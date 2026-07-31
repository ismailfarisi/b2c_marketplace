package ae.marketplace.mobile.me

import ae.marketplace.mobile.model.Inbox
import ae.marketplace.mobile.model.Inquiry
import ae.marketplace.mobile.model.Message
import ae.marketplace.mobile.model.MyListingsPage
import ae.marketplace.mobile.model.SavedPage
import ae.marketplace.mobile.model.SearchPage
import ae.marketplace.mobile.model.Thread
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.get
import io.ktor.client.request.parameter
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.Serializable

/**
 * Everything behind the three tabs that were stubs, plus the messaging the
 * reel's CTA has always pointed at.
 *
 * One class rather than four, because these are four small read surfaces for
 * one person — the signed-in viewer — and splitting them would produce four
 * files of six lines each and a constructor graph to match.
 */
class MeApi(private val client: HttpClient) {

    /** The Saved tab. */
    suspend fun saves(limit: Int = 50): SavedPage =
        client.get("me/saves") { parameter("limit", limit) }.body()

    /** The seller half of the Me tab, in every state including the ugly ones. */
    suspend fun myListings(limit: Int = 50): MyListingsPage =
        client.get("me/listings") { parameter("limit", limit) }.body()

    /**
     * Search, against the classifieds index rather than the feed.
     *
     * `lat`/`lng` are optional here and required on the feed, which is not an
     * inconsistency: the feed is local discovery and has no meaning without a
     * point, while a search for a specific car is a question someone will
     * happily answer nationwide. Passing them sorts by distance instead.
     */
    /**
     * The classifieds index.
     *
     * [filters] is passed whole rather than as a dozen parameters, so adding a
     * facet is a change in one data class instead of a change to this
     * signature, its call site, the view model and the screen.
     */
    suspend fun search(
        query: String,
        lat: Double? = null,
        lng: Double? = null,
        filters: SearchFilters = SearchFilters(),
        limit: Int = 20,
        offset: Int = 0,
    ): SearchPage = client.get("listings") {
        if (query.isNotBlank()) parameter("q", query)
        lat?.let { parameter("lat", it) }
        lng?.let { parameter("lng", it) }

        filters.priceMinMinor?.let { parameter("price_min_minor", it) }
        filters.priceMaxMinor?.let { parameter("price_max_minor", it) }
        filters.yearMin?.let { parameter("year_min", it) }
        filters.yearMax?.let { parameter("year_max", it) }
        filters.mileageMax?.let { parameter("mileage_max", it) }
        // Comma-separated, matching the server's csv() parsing. Empty sets are
        // omitted entirely rather than sent as an empty string, which the
        // server would read as a one-element list containing "".
        filters.regionalSpecs.takeIf { it.isNotEmpty() }
            ?.let { parameter("regional_spec", it.joinToString(",")) }
        filters.bodyTypes.takeIf { it.isNotEmpty() }
            ?.let { parameter("body_type", it.joinToString(",")) }
        filters.transmissions.takeIf { it.isNotEmpty() }
            ?.let { parameter("transmission", it.joinToString(",")) }
        filters.sellerKind?.let { parameter("seller_kind", it) }
        filters.sort?.let { parameter("sort", it) }

        parameter("limit", limit)
        parameter("offset", offset)
    }.body()

    suspend fun inbox(): Inbox = client.get("me/conversations").body()

    suspend fun thread(conversationId: String, limit: Int = 50): Thread =
        client.get("conversations/$conversationId/messages") {
            parameter("limit", limit)
        }.body()

    /**
     * Sends a message.
     *
     * The response is returned rather than discarded, because it carries the
     * `state` — and a `held` message has *not* been delivered. Dropping it and
     * optimistically drawing the bubble would tell the sender their message
     * arrived when a moderator still has it.
     */
    suspend fun send(conversationId: String, body: String): Message =
        client.post("conversations/$conversationId/messages") {
            contentType(ContentType.Application.Json)
            setBody(SendBody(body))
        }.body()

    suspend fun markRead(conversationId: String) {
        client.post("conversations/$conversationId/read")
    }

    /**
     * Opens (or reopens) the thread for a listing. This is where the reel's
     * "Message seller" CTA actually goes.
     *
     * [sourceReelId] and [sourceActionId] are the attribution the whole video
     * bet is measured by: they are what lets someone answer "did the feed
     * produce this lead" rather than guess. Omitting them makes the reel look
     * like it converted nothing.
     */
    suspend fun inquire(
        listingId: String,
        sourceReelId: String? = null,
        sourceActionId: String? = null,
    ): Inquiry = client.post("listings/$listingId/inquire") {
        contentType(ContentType.Application.Json)
        setBody(
            InquireBody(
                // The wire values the server accepts, not words of our own.
                // A lead opened from a reel is `reel_action` and one opened
                // from the results list is `search`, and the difference is the
                // number that says whether the feed is producing demand — file
                // them both as the same thing and the video bet becomes
                // unmeasurable.
                source = if (sourceReelId != null) "reel_action" else "search",
                sourceReelId = sourceReelId,
                sourceActionId = sourceActionId,
            )
        )
    }.body()
}

@Serializable
private data class SendBody(val body: String)

@Serializable
private data class InquireBody(
    val source: String,
    @kotlinx.serialization.SerialName("source_reel_id") val sourceReelId: String?,
    @kotlinx.serialization.SerialName("source_action_id") val sourceActionId: String?,
)
