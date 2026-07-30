package ae.marketplace.mobile.feed

import ae.marketplace.mobile.model.EventBatch
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

    suspend fun feed(
        lat: Double,
        lng: Double,
        cursor: String? = null,
        categoryId: String? = null,
        limit: Int = 10,
        surface: FeedSurface = FeedSurface.ForYou,
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
    }.body()

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
