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
    @SerialName("week_dishes") val weekDishes: List<DishThumb> = emptyList(),
    /** Weeks the menu runs before a dish repeats; above 1 the card says "No repeats for N weeks". */
    @SerialName("menu_weeks") val menuWeeks: Int = 1,
)

/** One of this week's default dishes, photographed ones first. */
@Serializable
data class DishThumb(
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    @SerialName("image_url") val imageUrl: String? = null,
    val cta: String? = null,
    @SerialName("cta_ar") val ctaAr: String? = null,
    /** A site path (`/kitchens/…`, opened in-app via MealsLinks) or a full URL. */
    @SerialName("link_url") val linkUrl: String? = null,
)

/** A dish on the next delivery day, from a kitchen that delivers to the customer. */
@Serializable
data class DayDish(
    @SerialName("branch_id") val branchId: String,
    @SerialName("kitchen_name") val kitchenName: String,
    @SerialName("kitchen_name_ar") val kitchenNameAr: String? = null,
    val name: String,
    @SerialName("name_ar") val nameAr: String? = null,
    @SerialName("image_url") val imageUrl: String? = null,
    val slot: String,
    @SerialName("has_alternative") val hasAlternative: Boolean = false,
    @SerialName("from_meal_price_minor") val fromMealPriceMinor: Long = 0,
    val currency: String = "AED",
    val rating: Double? = null,
    @SerialName("rating_count") val ratingCount: Int = 0,
)

@Serializable
data class NextDayDishes(
    val date: String,
    val weekday: Int,
    val items: List<DayDish> = emptyList(),
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
    @SerialName("menu_weeks") val menuWeeks: Int = 1,
    /** The menu over real dates: this week first, one entry per week up to [menuWeeks]. */
    @SerialName("menu_calendar") val menuCalendar: List<MenuWeek> = emptyList(),
    @SerialName("plan_list") val plans: List<MealPlan> = emptyList(),
    @SerialName("popular_plan_id") val popularPlanId: String? = null,
)

@Serializable
data class DeliveryWindow(
    val id: String,
    val label: String,
    @SerialName("starts_at") val startsAt: String = "",
    @SerialName("ends_at") val endsAt: String = "",
)

@Serializable
data class WeeklyMenu(
    val slots: List<MenuSlot> = emptyList(),
    val weeks: Int = 1,
    @SerialName("current_week") val currentWeek: Int = 1,
)

/** One calendar week of a kitchen's menu, from [startsOn] (a Sunday, yyyy-mm-dd). */
@Serializable
data class MenuWeek(
    @SerialName("starts_on") val startsOn: String,
    val slots: List<MenuSlot> = emptyList(),
)

@Serializable
data class MenuSlot(
    val weekday: Int,
    val slot: String,
    val default: MenuChoice? = null,
    val alternative: MenuChoice? = null,
    /** Week of the menu cycle, 1 on a one-week menu. */
    val week: Int = 1,
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
    /** cuisine_strip: the cuisines served near the customer. */
    val cuisines: List<CuisineTile>? = null,
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
    val subtitle: String? = null,
    @SerialName("subtitle_ar") val subtitleAr: String? = null,
)

@Serializable
data class Banner(
    val id: String,
    val title: String,
    @SerialName("title_ar") val titleAr: String? = null,
    val body: String? = null,
    @SerialName("body_ar") val bodyAr: String? = null,
    @SerialName("image_url") val imageUrl: String? = null,
    val cta: String? = null,
    @SerialName("cta_ar") val ctaAr: String? = null,
    @SerialName("link_url") val linkUrl: String? = null,
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
    /** Set when a company pays for this office lunch; never the employee's to pay. */
    @SerialName("company_name") val companyName: String? = null,
    @SerialName("rotation_id") val rotationId: String? = null,
    @SerialName("rotation_week") val rotationWeek: Int? = null,
    @SerialName("renewal_mode") val renewalMode: String = "manual",
    @SerialName("renewed_by_id") val renewedById: String? = null,
    @SerialName("can_renew") val canRenew: Boolean = false,
    @SerialName("pause_allowance_days") val pauseAllowanceDays: Int = 0,
    @SerialName("pause_days_used") val pauseDaysUsed: Int = 0,
    val upcoming: List<UpcomingMeal> = emptyList(),
    /** Skipped meals in the same window; they can be put back before the cutoff. */
    val skipped: List<UpcomingMeal> = emptyList(),
    /** The last two weeks' deliveries, newest first. */
    val recent: List<MyDelivery> = emptyList(),
    /** Current and upcoming pauses. */
    val pauses: List<MyPause> = emptyList(),
)

@Serializable
data class MyPause(
    @SerialName("from_date") val fromDate: String,
    @SerialName("to_date") val toDate: String,
)

@Serializable
data class MyDelivery(
    val date: String,
    @SerialName("meal_slot") val mealSlot: String = "",
    val status: String,
    val at: String? = null,
)

@Serializable
data class Resumed(
    @SerialName("resumes_on") val resumesOn: String,
    @SerialName("days_returned") val daysReturned: Int = 0,
)

@Serializable
data class MyReview(
    @SerialName("can_review") val canReview: Boolean = false,
    val reason: String? = null,
    val review: SubscriptionReview? = null,
)

@Serializable
data class SubscriptionReview(
    val id: String = "",
    val rating: Int,
    val comment: String = "",
    val status: String = "",
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
    /** scheduled, dispatched, en_route, arrived, completed or failed, once a delivery exists. */
    @SerialName("delivery_status") val deliveryStatus: String? = null,
    val skipped: Boolean = false,
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
    val status: String? = null,
    /** The customer can still change this week's kitchen (until [swapUntil]). */
    val swappable: Boolean = false,
    /** This week's kitchen was changed once already. */
    val swapped: Boolean = false,
    @SerialName("swap_until") val swapUntil: String? = null,
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
    /** Renewals propose fresh kitchens instead of the same four. */
    @SerialName("refresh_on_renew") val refreshOnRenew: Boolean = false,

    /** Advisory variety check from preview; it never blocks buying. */
    val variety: RotationVariety? = null,
) {
    /** Created, not paid yet: the card offers to pay. */
    val awaitingPayment: Boolean get() = status == "pending_payment" || status == "pending"

    /** Running or awaiting payment: renewal can change and it can be cancelled. */
    val live: Boolean get() = status == "active" || awaitingPayment

    /** Paid and ran (or running), not renewed yet: what POST rotations/{id}/renew accepts. */
    val renewable: Boolean get() = renewedById == null && (status == "active" || status == "completed")
}

@Serializable
data class RotationVariety(
    /** low, good or great. */
    val level: String,
    val score: Int = 0,
    @SerialName("distinct_cuisines") val distinctCuisines: Int = 0,
    val warnings: List<RotationVarietyWarning> = emptyList(),
) {
    /** How many of the meter's three steps are lit. */
    val steps: Int get() = when (level) { "great" -> 3; "good" -> 2; else -> 1 }
}

@Serializable
data class RotationVarietyWarning(
    /** same_cuisine, repeated_dish or repeated_across_weeks. */
    val code: String,
    val cuisines: List<String> = emptyList(),
    val dish: String = "",
    @SerialName("dish_ar") val dishAr: String? = null,
    /** repeated_dish only; 0 = Sunday. */
    val weekday: Int? = null,
    val kitchens: Int = 0,
)

@Serializable
data class RotationSuggestionInput(
    @SerialName("address_id") val addressId: String,
    @SerialName("promo_code") val promoCode: String? = null,
)

@Serializable
data class RotationSuggestion(
    @SerialName("plan_ids") val planIds: List<String>,
    @SerialName("branch_ids") val branchIds: List<String>,
    @SerialName("start_date") val startDate: String,
    @SerialName("delivery_weekdays") val deliveryWeekdays: List<Int>,
    @SerialName("meal_slots") val mealSlots: List<String>,
    val quote: Rotation,
)

@Serializable
data class RotationPage(val items: List<Rotation> = emptyList())

// ── Variety at renewal: proposal, taste memory, adventure setting ──────────

/** One kitchen the next rotation could use, priced for one week of this rotation's meals. */
@Serializable
data class ProposalKitchen(
    @SerialName("branch_id") val branchId: String,
    @SerialName("plan_id") val planId: String,
    @SerialName("kitchen_name") val kitchenName: String,
    @SerialName("kitchen_name_ar") val kitchenNameAr: String? = null,
    val cuisines: List<String> = emptyList(),
    val rating: Double? = null,
    @SerialName("rating_count") val ratingCount: Int = 0,
    @SerialName("week_price_minor") val weekPriceMinor: Long,
    @SerialName("meal_price_minor") val mealPriceMinor: Long = 0,
    val currency: String = "AED",
    @SerialName("in_current") val inCurrent: Boolean = false,
    val favourite: Boolean = false,
    @SerialName("own_rating") val ownRating: Int = 0,
    /** loved, top_rated, kept, new_cuisine, familiar_cuisine, new_kitchen or empty. */
    val reason: String = "",
    @SerialName("reason_cuisine") val reasonCuisine: String? = null,
)

@Serializable
data class RenewalProposal(
    @SerialName("rotation_id") val rotationId: String,
    @SerialName("start_date") val startDate: String,
    val adventure: String = "balanced",
    @SerialName("refresh_on_renew") val refreshOnRenew: Boolean = false,
    /** The four proposed, favourites first. */
    val kitchens: List<ProposalKitchen> = emptyList(),
    /** Every kitchen that can be swapped in, the current ones included. */
    val eligible: List<ProposalKitchen> = emptyList(),
    @SerialName("amount_minor") val amountMinor: Long = 0,
    @SerialName("previous_amount_minor") val previousAmountMinor: Long = 0,
    val currency: String = "AED",
    @SerialName("meals_per_week") val mealsPerWeek: Int = 0,
)

/** A heart or a "not again" on a kitchen or a dish. */
@Serializable
data class Taste(
    @SerialName("subject_kind") val subjectKind: String,
    @SerialName("subject_id") val subjectId: String,
    val sentiment: String,
    @SerialName("updated_at") val updatedAt: String = "",
)

@Serializable
data class TasteInput(
    @SerialName("subject_kind") val subjectKind: String,
    @SerialName("subject_id") val subjectId: String,
    val sentiment: String,
)

@Serializable
data class TastePage(val items: List<Taste> = emptyList())

/** familiar, balanced or surprise. */
@Serializable
data class MealPreferences(val adventure: String = "balanced")

/** A kitchen one rotation week could change to, and what that costs or credits. */
@Serializable
data class RotationSwapOption(
    @SerialName("branch_id") val branchId: String,
    @SerialName("kitchen_name") val kitchenName: String,
    @SerialName("kitchen_name_ar") val kitchenNameAr: String? = null,
    val cuisines: List<String> = emptyList(),
    @SerialName("cover_image_url") val coverImageUrl: String? = null,
    val rating: Double? = null,
    @SerialName("rating_count") val ratingCount: Int = 0,
    @SerialName("plan_id") val planId: String,
    @SerialName("plan_name") val planName: String = "",
    @SerialName("price_minor") val priceMinor: Long,
    /** price_minor less the week's current price: positive is paid now, negative goes to the wallet. */
    @SerialName("difference_minor") val differenceMinor: Long,
    val currency: String = "AED",
)

@Serializable
data class RotationSwapOptions(
    val week: Int,
    @SerialName("current_price_minor") val currentPriceMinor: Long,
    @SerialName("swap_until") val swapUntil: String = "",
    val currency: String = "AED",
    val items: List<RotationSwapOption> = emptyList(),
)

@Serializable
data class RotationSwapResult(
    val rotation: Rotation,
    @SerialName("due_minor") val dueMinor: Long = 0,
    val currency: String = "AED",
    /** Collects dueMinor when the new kitchen costs more. */
    val payment: PaymentIntent? = null,
)
