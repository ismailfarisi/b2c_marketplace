package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.model.AddressInput
import ae.marketplace.mobile.model.AddressPage
import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.KitchenPage
import ae.marketplace.mobile.model.LatLng
import ae.marketplace.mobile.model.MealsHome
import ae.marketplace.mobile.model.MySubscription
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

    suspend fun kitchens(at: LatLng, slot: String? = null, rotation: Boolean = false): KitchenPage =
        client.get("meals/kitchens") {
            point(at)
            slot?.let { parameter("slot", it) }
            if (rotation) parameter("rotation", "true")
        }.body()

    suspend fun kitchen(branchId: String, at: LatLng): KitchenDetail =
        client.get("meals/kitchens/$branchId") { point(at) }.body()

    suspend fun reviews(branchId: String): ReviewPage =
        client.get("meals/kitchens/$branchId/reviews").body()

    suspend fun addresses(): AddressPage = client.get("me/addresses").body()

    suspend fun createAddress(input: AddressInput): Address =
        client.post("me/addresses") { json(input) }.body()

    suspend fun preview(input: SubscribeInput): SubscriptionPreview =
        client.post("subscriptions/preview") { json(input) }.body()

    suspend fun subscribe(input: SubscribeInput): Subscribed =
        client.post("subscriptions") { json(input) }.body()

    /** Keyed on the order, so a retry re-reads the same authorization instead of charging twice. */
    suspend fun payOrder(orderId: String): PaymentIntent =
        client.post("orders/$orderId/payment-intent") {
            header("Idempotency-Key", orderId)
            json(Empty())
        }.body()

    suspend fun subscriptions(): SubscriptionPage = client.get("me/subscriptions").body()

    suspend fun subscription(id: String): MySubscription = client.get("me/subscriptions/$id").body()

    suspend fun choose(id: String, date: String, slot: String, dishId: String) {
        client.put("subscriptions/$id/meals/$date/$slot") { json(DishBody(dishId)) }
    }

    suspend fun skip(id: String, date: String, slot: String) {
        client.post("subscriptions/$id/skip") { json(SkipBody(date, slot)) }
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

    suspend fun createRotation(input: RotationInput): Rotation =
        client.post("rotations") { json(input) }.body()

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
