package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.EventBatch
import ae.marketplace.mobile.model.FeedFilters
import ae.marketplace.mobile.model.FeedItem
import ae.marketplace.mobile.model.FeedPage
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.get
import io.ktor.client.request.parameter
import io.ktor.client.request.delete
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType

/**
 * The feed endpoints, as this client uses them.
 *
 * `lat` and `lng` are non-null parameters rather than optional ones, mirroring
 * the spec: *"the product is local discovery; a feed without a location is a
 * different product"*. Making them nullable here would push that decision into
 * every caller and eventually someone would default them.
 */
class FeedApi(private val client: HttpClient) {

    /**
     * One reel, enriched the same way a feed page is.
     *
     * The location is optional here, unlike [feed], and the asymmetry is the
     * point: "what is near me" has no answer without a point, while "show me
     * the car I saved" has the same answer everywhere. The coordinates only
     * affect whether the CTA can say it does not reach you.
     */
    suspend fun reel(reelId: String, lat: Double?, lng: Double?): FeedItem =
        client.get("feed/reels/$reelId") {
            lat?.let { parameter("lat", it) }
            lng?.let { parameter("lng", it) }
        }.body()

    suspend fun feed(
        lat: Double,
        lng: Double,
        cursor: String? = null,
        categoryId: String? = null,
        limit: Int = 10,
        surface: FeedSurface = FeedSurface.ForYou,
        subjectType: String? = null,
    ): FeedPage = client.get("feed") {
        parameter("lat", lat)
        parameter("lng", lng)
        parameter("limit", limit)
        // Always sent, including the default. The server treats an absent and an
        // unrecognised value identically, so sending it explicitly costs nothing
        // and makes a request log answer "which tab was this" without inference.
        parameter("surface", surface.wire)
        cursor?.let { parameter("cursor", it) }
        categoryId?.let { parameter("category", it) }
        // Omitted entirely when null. Sending an empty value would be a filter
        // that matches nothing rather than the absence of a filter.
        subjectType?.let { parameter("subject_type", it) }
    }.body()

    /**
     * The filter row's contents.
     *
     * Fetched once per session rather than per page: the set changes when a
     * vertical is deployed, not while someone is scrolling.
     */
    suspend fun filters(): FeedFilters = client.get("feed/filters").body()

    suspend fun submitEvents(batch: EventBatch) {
        client.post("events") {
            contentType(ContentType.Application.Json)
            setBody(batch)
        }
    }

    suspend fun clickAction(actionId: String) {
        client.post("actions/$actionId/click")
    }

    suspend fun like(reelId: String) {
        client.put("reels/$reelId/like")
    }

    suspend fun unlike(reelId: String) {
        client.delete("reels/$reelId/like")
    }
}
