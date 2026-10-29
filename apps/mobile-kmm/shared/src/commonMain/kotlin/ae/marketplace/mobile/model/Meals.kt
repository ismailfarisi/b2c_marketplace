package ae.marketplace.mobile.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlin.math.roundToInt
import kotlin.math.roundToLong

/**
 * The meals storefront and a customer's subscriptions, hand-written from
 * `api/openapi.yaml` like the rest of this package. Money is in minor units.
 * Weekday 0 is Sunday, as everywhere in the API.
 */

@Serializable
data class LatLng(val lat: Double, val lng: Double)

@Serializable
data class Kitchen(
    @SerialName("branch_id") val branchId: String,
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    @SerialName("vendor_name") val vendorName: String = "",
    val description: String? = null,
    @SerialName("description_ar") val descriptionAr: String? = null,
    val cuisines: List<String> = emptyList(),
    @SerialName("dietary_tags") val dietaryTags: List<String> = emptyList(),
    @SerialName("cover_image_url") val coverImageUrl: String? = null,
    val city: String? = null,
    val rating: Double? = null,
    @SerialName("rating_count") val ratingCount: Int = 0,
    val currency: String = "AED",
    @SerialName("meal_slots") val mealSlots: List<String> = emptyList(),
    @SerialName("from_price_minor") val fromPriceMinor: Long = 0,
    @SerialName("from_meal_price_minor") val fromMealPriceMinor: Long = 0,
    @SerialName("rotation_week_price_minor") val rotationWeekPriceMinor: Long? = null,
    @SerialName("food_safety_verified") val foodSafetyVerified: Boolean = false,
)

@Serializable
data class KitchenDetail(
    @SerialName("branch_id") val branchId: String,
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    val description: String? = null,
    @SerialName("description_ar") val descriptionAr: String? = null,
    val cuisines: List<String> = emptyList(),
    @SerialName("cover_image_url") val coverImageUrl: String? = null,
    val city: String? = null,
    val rating: Double? = null,
    @SerialName("rating_count") val ratingCount: Int = 0,
    val currency: String = "AED",
    @SerialName("from_meal_price_minor") val fromMealPriceMinor: Long = 0,
    @SerialName("food_safety_verified") val foodSafetyVerified: Boolean = false,
    @SerialName("delivers_here") val deliversHere: Boolean? = null,
    @SerialName("delivery_windows") val deliveryWindows: List<DeliveryWindow> = emptyList(),
    val menu: WeeklyMenu = WeeklyMenu(),
    @SerialName("plan_list") val plans: List<MealPlan> = emptyList(),
)

@Serializable
data class DeliveryWindow(
    val id: String,
    val label: String,
    @SerialName("starts_at") val startsAt: String = "",
    @SerialName("ends_at") val endsAt: String = "",
)

@Serializable
data class WeeklyMenu(val slots: List<MenuSlot> = emptyList())

@Serializable
data class MenuSlot(
    val weekday: Int,
    val slot: String,
    val default: MenuChoice? = null,
    val alternative: MenuChoice? = null,
)

@Serializable
data class MenuChoice(
    @SerialName("dish_id") val dishId: String,
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    val allergens: List<String> = emptyList(),
    val calories: Int? = null,
    @SerialName("image_url") val imageUrl: String? = null,
)

@Serializable
data class MealPlan(
    val id: String,
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    val description: String? = null,
    @SerialName("description_ar") val descriptionAr: String? = null,
    @SerialName("cycle_days") val cycleDays: Int,
    @SerialName("days_per_week") val daysPerWeek: Int,
    @SerialName("meals_per_day") val mealsPerDay: Int,
    @SerialName("meal_slots") val mealSlots: List<String> = emptyList(),
    @SerialName("delivery_weekdays") val deliveryWeekdays: List<Int> = emptyList(),
    @SerialName("allowed_pause_days") val allowedPauseDays: Int = 0,
    @SerialName("cutoff_hours") val cutoffHours: Int = 0,
    @SerialName("price_minor") val priceMinor: Long,
    val currency: String = "AED",
    @SerialName("rotation_enabled") val rotationEnabled: Boolean = false,
    @SerialName("rotation_week_price_minor") val rotationWeekPriceMinor: Long? = null,
) {
    /** Meals delivered in one cycle; the per-meal price divides by this, as on the web. */
    val mealsPerCycle: Int
        get() = maxOf(1, (cycleDays * daysPerWeek / 7.0).roundToInt() * mealsPerDay)

    val mealPriceMinor: Long get() = (priceMinor.toDouble() / mealsPerCycle).roundToLong()
}

@Serializable
data class KitchenPage(val items: List<Kitchen> = emptyList())

@Serializable
data class MealsHome(
    @SerialName("version_id") val versionId: String? = null,
    val sections: List<HomeSection>? = null,
)

@Serializable
data class HomeSection(
    val id: String,
    val type: String,
    val enabled: Boolean = true,
    val settings: SectionSettings = SectionSettings(),
    val collection: Collection? = null,
    val kitchens: List<Kitchen>? = null,
    val banners: List<Banner>? = null,
)

@Serializable
data class SectionSettings(
    val title: String? = null,
    @SerialName("title_ar") val titleAr: String? = null,
)

@Serializable
data class Collection(
    val slug: String,
    val title: String,
    @SerialName("title_ar") val titleAr: String? = null,
)

@Serializable
data class Banner(
    val id: String,
    val title: String,
    @SerialName("title_ar") val titleAr: String? = null,
    val body: String? = null,
    @SerialName("body_ar") val bodyAr: String? = null,
    @SerialName("image_url") val imageUrl: String? = null,
)

@Serializable
data class KitchenReview(
    val id: String,
    val rating: Int,
    val comment: String = "",
    val reply: String? = null,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class ReviewPage(val items: List<KitchenReview> = emptyList())

@Serializable
data class Address(
    val id: String,
    val label: String,
    val building: String? = null,
    val flat: String? = null,
    val street: String? = null,
    val area: String? = null,
    val city: String = "",
    val point: LatLng,
    @SerialName("is_default") val isDefault: Boolean = false,
) {
    val line: String get() = listOfNotNull(flat, building, street, area, city)
        .filter { it.isNotBlank() }.joinToString(", ")
}

@Serializable
data class AddressPage(val items: List<Address> = emptyList())

@Serializable
data class AddressInput(
    val label: String,
    val building: String? = null,
    val flat: String? = null,
    val street: String? = null,
    val area: String? = null,
    val city: String,
    val point: LatLng,
    @SerialName("is_default") val isDefault: Boolean = false,
)

@Serializable
data class ScheduleRule(
    val weekday: Int,
    @SerialName("meal_slot") val mealSlot: String,
    @SerialName("window_id") val windowId: String,
)

@Serializable
data class SubscribeInput(
    @SerialName("plan_id") val planId: String,
    @SerialName("address_id") val addressId: String,
    @SerialName("start_date") val startDate: String,
    val schedule: List<ScheduleRule>,
    @SerialName("promo_code") val promoCode: String? = null,
)

@Serializable
data class SubscriptionPreview(
    @SerialName("subtotal_minor") val subtotalMinor: Long,
    @SerialName("discount_minor") val discountMinor: Long = 0,
    @SerialName("delivery_minor") val deliveryMinor: Long = 0,
    @SerialName("total_minor") val totalMinor: Long,
    val currency: String = "AED",
    @SerialName("starts_on") val startsOn: String = "",
    @SerialName("ends_on") val endsOn: String = "",
    @SerialName("deliverable_days") val deliverableDays: Int = 0,
)

@Serializable
data class Subscribed(
    val id: String,
    @SerialName("order_id") val orderId: String,
    @SerialName("amount_minor") val amountMinor: Long = 0,
    val currency: String = "AED",
)

@Serializable
data class PaymentIntent(
    val id: String,
    val status: String,
    @SerialName("redirect_url") val redirectUrl: String? = null,
)

@Serializable
data class MySubscription(
    val id: String,
    @SerialName("subscription_no") val subscriptionNo: String = "",
    val status: String,
    @SerialName("branch_id") val branchId: String = "",
    @SerialName("kitchen_name") val kitchenName: String,
    @SerialName("kitchen_name_ar") val kitchenNameAr: String? = null,
    @SerialName("plan_name") val planName: String = "",
    @SerialName("plan_name_ar") val planNameAr: String? = null,
    @SerialName("start_date") val startDate: String = "",
    @SerialName("end_date") val endDate: String? = null,
    @SerialName("cycle_ends_on") val cycleEndsOn: String? = null,
    @SerialName("amount_minor") val amountMinor: Long = 0,
    val currency: String = "AED",
    @SerialName("pending_order_id") val pendingOrderId: String? = null,
    @SerialName("rotation_id") val rotationId: String? = null,
    @SerialName("rotation_week") val rotationWeek: Int? = null,
    @SerialName("renewal_mode") val renewalMode: String = "manual",
    @SerialName("renewed_by_id") val renewedById: String? = null,
    @SerialName("can_renew") val canRenew: Boolean = false,
    @SerialName("pause_allowance_days") val pauseAllowanceDays: Int = 0,
    @SerialName("pause_days_used") val pauseDaysUsed: Int = 0,
    val upcoming: List<UpcomingMeal> = emptyList(),
)

@Serializable
data class SubscriptionPage(val items: List<MySubscription> = emptyList())

@Serializable
data class MealOption(
    val id: String,
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    val calories: Int? = null,
    @SerialName("is_default") val isDefault: Boolean = false,
)

@Serializable
data class UpcomingMeal(
    val date: String,
    val weekday: Int,
    @SerialName("meal_slot") val mealSlot: String,
    @SerialName("window_label") val windowLabel: String = "",
    val options: List<MealOption> = emptyList(),
    @SerialName("chosen_dish_id") val chosenDishId: String? = null,
    @SerialName("cutoff_at") val cutoffAt: String = "",
    val locked: Boolean = false,
)

@Serializable
data class RotationInput(
    @SerialName("plan_ids") val planIds: List<String>,
    @SerialName("address_id") val addressId: String,
    @SerialName("start_date") val startDate: String,
    @SerialName("delivery_weekdays") val deliveryWeekdays: List<Int>,
    @SerialName("meal_slots") val mealSlots: List<String>,
    @SerialName("promo_code") val promoCode: String? = null,
)

@Serializable
data class RotationWeek(
    val week: Int,
    @SerialName("starts_on") val startsOn: String,
    @SerialName("ends_on") val endsOn: String,
    @SerialName("kitchen_name") val kitchenName: String,
    @SerialName("kitchen_name_ar") val kitchenNameAr: String? = null,
    @SerialName("price_minor") val priceMinor: Long,
    @SerialName("subscription_id") val subscriptionId: String? = null,
)

@Serializable
data class Rotation(
    val id: String = "",
    @SerialName("rotation_no") val rotationNo: String = "",
    val status: String = "",
    @SerialName("start_date") val startDate: String,
    @SerialName("end_date") val endDate: String,
    val weeks: List<RotationWeek>,
    @SerialName("amount_minor") val amountMinor: Long,
    @SerialName("discount_minor") val discountMinor: Long = 0,
    val currency: String = "AED",
    @SerialName("auto_renew") val autoRenew: Boolean = false,
    @SerialName("renewed_by_id") val renewedById: String? = null,
)

@Serializable
data class RotationPage(val items: List<Rotation> = emptyList())
