package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.CollectionKitchens
import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.model.NextDayDishes
import ae.marketplace.mobile.model.AddressInput
import ae.marketplace.mobile.model.AddressPage
import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.KitchenPage
import ae.marketplace.mobile.model.LatLng
import ae.marketplace.mobile.model.MealsHome
import ae.marketplace.mobile.model.MyReview
import ae.marketplace.mobile.model.MySubscription
import ae.marketplace.mobile.model.OfferPage
import ae.marketplace.mobile.model.Resumed
import ae.marketplace.mobile.model.PaymentIntent
import ae.marketplace.mobile.model.ReviewPage
import ae.marketplace.mobile.model.Rotation
import ae.marketplace.mobile.model.RotationInput
import ae.marketplace.mobile.model.RotationPage
import ae.marketplace.mobile.model.SubscribeInput
import ae.marketplace.mobile.model.Subscribed
import ae.marketplace.mobile.model.SubscriptionPage
import ae.marketplace.mobile.model.SubscriptionPreview
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.HttpRequestBuilder
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.request.parameter
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** The meals storefront (anonymous) and the signed-in customer's subscriptions. */
class MealsApi(private val client: HttpClient) {

    suspend fun home(at: LatLng): MealsHome = client.get("meals/home") { point(at) }.body()

    suspend fun tomorrowDishes(at: LatLng): NextDayDishes =
        client.get("meals/dishes/tomorrow") { point(at) }.body()

    suspend fun kitchens(
        at: LatLng,
        slot: String? = null,
        rotation: Boolean = false,
        q: String? = null,
        cuisine: String? = null,
    ): KitchenPage =
        client.get("meals/kitchens") {
            point(at)
            slot?.let { parameter("slot", it) }
            if (rotation) parameter("rotation", "true")
            q?.takeIf { it.isNotBlank() }?.let { parameter("q", it) }
            cuisine?.let { parameter("cuisine", it) }
        }.body()

    suspend fun collection(slug: String, at: LatLng): CollectionKitchens =
        client.get("meals/collections/$slug") { point(at) }.body()

    suspend fun kitchen(branchId: String, at: LatLng): KitchenDetail =
        client.get("meals/kitchens/$branchId") { point(at) }.body()

    suspend fun reviews(branchId: String): ReviewPage =
        client.get("meals/kitchens/$branchId/reviews").body()

    /** Public promos and trials usable at the kitchen now. */
    suspend fun offers(branchId: String): OfferPage =
        client.get("meals/kitchens/$branchId/offers").body()

    suspend fun addresses(): AddressPage = client.get("me/addresses").body()

    suspend fun createAddress(input: AddressInput): Address =
        client.post("me/addresses") { json(input) }.body()

    suspend fun preview(input: SubscribeInput): SubscriptionPreview =
        client.post("subscriptions/preview") { json(input) }.body()

    /** [idempotencyKey] is one UUID per checkout draft, so a retried Pay returns the same subscription. */
    suspend fun subscribe(input: SubscribeInput, idempotencyKey: String? = null): Subscribed =
        client.post("subscriptions") {
            idempotencyKey?.let { header("Idempotency-Key", it) }
            json(input)
        }.body()

    /** Keyed on the order, so a retry re-reads the same authorization instead of charging twice. */
    suspend fun payOrder(orderId: String): PaymentIntent =
        client.post("orders/$orderId/payment-intent") {
            header("Idempotency-Key", orderId)
            json(Empty())
        }.body()

    suspend fun subscriptions(): SubscriptionPage = client.get("me/subscriptions").body()

    suspend fun myCompanies(): ae.marketplace.mobile.model.CompanyPage = client.get("me/companies").body()

    suspend fun subscription(id: String): MySubscription = client.get("me/subscriptions/$id").body()

    suspend fun choose(id: String, date: String, slot: String, dishId: String) {
        client.put("subscriptions/$id/meals/$date/$slot") { json(DishBody(dishId)) }
    }

    suspend fun skip(id: String, date: String, slot: String) {
        client.post("subscriptions/$id/skip") { json(SkipBody(date, slot)) }
    }

    suspend fun unskip(id: String, date: String, slot: String) {
        client.post("subscriptions/$id/unskip") { json(SkipBody(date, slot)) }
    }

    /** Ends the current or next pause from the first day still before its cutoff. */
    suspend fun resume(id: String): Resumed =
        client.post("subscriptions/$id/resume") { json(Empty()) }.body()

    suspend fun myReview(id: String): MyReview = client.get("me/subscriptions/$id/review").body()

    suspend fun writeReview(id: String, rating: Int, comment: String) {
        client.put("subscriptions/$id/review") { json(ReviewBody(rating, comment)) }
    }

    suspend fun pause(id: String, from: String, to: String) {
        client.post("subscriptions/$id/pause") { json(PauseBody(from, to)) }
    }

    suspend fun cancel(id: String, reason: String) {
        client.post("subscriptions/$id/cancel") { json(ReasonBody(reason)) }
    }

    suspend fun setRenewal(id: String, auto: Boolean) {
        client.put("subscriptions/$id/renewal") { json(AutoBody(auto)) }
    }

    suspend fun renew(id: String): Subscribed = client.post("subscriptions/$id/renew") { json(Empty()) }.body()

    suspend fun previewRotation(input: RotationInput): Rotation =
        client.post("rotations/preview") { json(input) }.body()

    /** [idempotencyKey] is one UUID per builder draft, so a retried create is not a second rotation. */
    suspend fun createRotation(input: RotationInput, idempotencyKey: String): Rotation =
        client.post("rotations") {
            header("Idempotency-Key", idempotencyKey)
            json(input)
        }.body()

    /** Prepares the next rotation (same kitchens, next four weeks), unpaid. */
    suspend fun renewRotation(id: String): Rotation =
        client.post("rotations/$id/renew") { json(Empty()) }.body()

    suspend fun payRotation(id: String): PaymentIntent =
        client.post("rotations/$id/payment-intent") {
            header("Idempotency-Key", id)
            json(Empty())
        }.body()

    suspend fun rotations(): RotationPage = client.get("me/rotations").body()

    suspend fun rotation(id: String): Rotation = client.get("me/rotations/$id").body()

    suspend fun cancelRotation(id: String, reason: String) {
        client.post("rotations/$id/cancel") { json(ReasonBody(reason)) }
    }

    suspend fun setRotationRenewal(id: String, auto: Boolean) {
        client.put("rotations/$id/renewal") { json(AutoBody(auto)) }
    }

    private fun HttpRequestBuilder.point(at: LatLng) {
        parameter("lat", at.lat)
        parameter("lng", at.lng)
    }

    private inline fun <reified T> HttpRequestBuilder.json(body: T) {
        contentType(ContentType.Application.Json)
        setBody(body)
    }
}

@Serializable private class Empty
@Serializable private data class DishBody(@SerialName("dish_id") val dishId: String)
@Serializable private data class SkipBody(val date: String, @SerialName("meal_slot") val mealSlot: String)
@Serializable private data class PauseBody(@SerialName("from_date") val from: String, @SerialName("to_date") val to: String)
@Serializable private data class ReasonBody(val reason: String)
@Serializable private data class AutoBody(val auto: Boolean)
@Serializable private data class ReviewBody(val rating: Int, val comment: String)
