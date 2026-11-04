package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** A public promo or trial usable at a kitchen right now (GET /meals/kitchens/{id}/offers). */
@Serializable
data class PromoOffer(
    val id: String,
    val code: String? = null,
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    val description: String? = null,
    @SerialName("description_ar") val descriptionAr: String? = null,
    /** percent, fixed or none (a trial with no discount). */
    @SerialName("discount_type") val discountType: String = "none",
    /** Basis points: 2000 = 20%. */
    @SerialName("percent_bp") val percentBp: Int = 0,
    @SerialName("amount_minor") val amountMinor: Long = 0,
    @SerialName("max_discount_minor") val maxDiscountMinor: Long? = null,
    val currency: String = "AED",
    @SerialName("trial_days") val trialDays: Int? = null,
    /** subscriptions, rotations or both. */
    @SerialName("applies_to") val appliesTo: String = "both",
    /** everyone or new_customers. */
    val audience: String = "everyone",
    @SerialName("ends_at") val endsAt: String? = null,
) {
    val newCustomersOnly: Boolean get() = audience == "new_customers"
}

@Serializable
data class OfferPage(val items: List<PromoOffer> = emptyList())
