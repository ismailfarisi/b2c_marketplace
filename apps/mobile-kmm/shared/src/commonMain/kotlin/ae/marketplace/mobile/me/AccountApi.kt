package ae.marketplace.mobile.me

import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.model.AddressPage
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.delete
import io.ktor.client.request.get
import io.ktor.client.request.patch
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** The account half of Me: profile, saved addresses and marketing switches. */
class AccountApi(private val client: HttpClient) {

    suspend fun profile(): Profile = client.get("me").body()

    suspend fun updateProfile(input: ProfileInput): Profile =
        client.patch("me") {
            contentType(ContentType.Application.Json)
            setBody(input)
        }.body()

    suspend fun addresses(): AddressPage = client.get("me/addresses").body()

    suspend fun deleteAddress(id: String) {
        client.delete("me/addresses/$id")
    }

    suspend fun setDefaultAddress(id: String): Address =
        client.patch("me/addresses/$id") {
            contentType(ContentType.Application.Json)
            setBody(DefaultBody(true))
        }.body()

    suspend fun preferences(): PreferencePage = client.get("me/notification-preferences").body()

    /** familiar, balanced or surprise: how adventurous renewal proposals are. */
    suspend fun mealPreferences(): MealPreferences = client.get("me/meal-preferences").body()

    suspend fun setMealPreferences(adventure: String): MealPreferences =
        client.put("me/meal-preferences") {
            contentType(ContentType.Application.Json)
            setBody(MealPreferences(adventure))
        }.body()

    suspend fun setPreference(category: String, channel: String, allowed: Boolean) {
        client.put("me/notification-preferences") {
            contentType(ContentType.Application.Json)
            setBody(PreferenceBody(category, channel, allowed))
        }
    }
}

@Serializable
data class Profile(
    val id: String,
    val phone: String,
    val email: String? = null,
    @SerialName("display_name") val displayName: String? = null,
    val locale: String = "en",
)

@Serializable
data class ProfileInput(
    @SerialName("display_name") val displayName: String? = null,
    val email: String? = null,
)

@Serializable
data class NotificationPreference(
    val category: String,
    val channel: String,
    val allowed: Boolean,
    val optional: Boolean = true,
)

@Serializable
data class PreferencePage(val items: List<NotificationPreference> = emptyList())

@Serializable
data class MealPreferences(val adventure: String = "balanced")

@Serializable
private data class DefaultBody(@SerialName("is_default") val isDefault: Boolean)

@Serializable
private data class PreferenceBody(val category: String, val channel: String, val allowed: Boolean)
