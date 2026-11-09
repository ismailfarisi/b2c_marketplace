package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.CollectionKitchens
import ae.marketplace.mobile.model.DayDish
import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.model.AddressInput
import ae.marketplace.mobile.model.DeliveryAddress
import ae.marketplace.mobile.model.MealSlots
import ae.marketplace.mobile.model.SlotAddress
import ae.marketplace.mobile.model.HomeSection
import ae.marketplace.mobile.model.Kitchen
import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.KitchenReview
import ae.marketplace.mobile.model.LatLng
import ae.marketplace.mobile.model.MealPlan
import ae.marketplace.mobile.model.MyReview
import ae.marketplace.mobile.model.MySubscription
import ae.marketplace.mobile.model.ProposalKitchen

import ae.marketplace.mobile.model.GuestWeekResult
import ae.marketplace.mobile.model.CheaperPlans
import ae.marketplace.mobile.model.PromoOffer
import ae.marketplace.mobile.model.RenewalProposal
import ae.marketplace.mobile.model.Rotation
import ae.marketplace.mobile.model.RotationSwapOptions

import ae.marketplace.mobile.model.NextWeekPreview
import ae.marketplace.mobile.model.RotationRecap
import ae.marketplace.mobile.model.RotationInput
import ae.marketplace.mobile.model.RotationSuggestionInput
import ae.marketplace.mobile.model.ScheduleRule
import ae.marketplace.mobile.model.SubscribeInput
import ae.marketplace.mobile.model.SubscriptionPreview
import ae.marketplace.mobile.network.ApiException
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.orNullUnlessCancelled
import ae.marketplace.mobile.network.toFailure
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.datetime.DatePeriod
import kotlinx.datetime.LocalDate
import kotlinx.datetime.TimeZone
import kotlinx.datetime.plus
import kotlinx.datetime.toLocalDateTime
import kotlin.time.Clock
import kotlin.time.ExperimentalTime
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

data class StorefrontUiState(
    val at: LatLng? = null,
    val slot: String? = null,
    val sections: List<HomeSection> = emptyList(),
    val kitchens: List<Kitchen> = emptyList(),
    /** The next delivery day's dishes; empty when that call fails, which never blocks the page. */
    val dishes: List<DayDish> = emptyList(),
    /** The signed-in customer's saved addresses, for the location picker. */
    val addresses: List<Address> = emptyList(),
    val loading: Boolean = false,
    val error: Failure? = null,
    /** Kitchen or dish search, sent as `q`. */
    val query: String = "",
    val cuisine: String? = null,
    val rotationOnly: Boolean = false,
    /** Kitchens near [at] before any filter, so the cuisine strip stays put while filtering. */
    val unfilteredCount: Int = 0,
    /** The place and filters [kitchens] were loaded for; null when nothing is shown. */
    val loadedFor: StoreQuery? = null,
) {
    val filtering: Boolean
        get() = slot != null || query.isNotBlank() || cuisine != null || rotationOnly

    /** The list on screen is for an earlier place or filter (a refetch is running or failed). */
    val stale: Boolean
        get() = loadedFor != null && at != null && loadedFor != StoreQuery.of(this)

    /** Enough rotation kitchens deliver here for the rotation promo to lead somewhere. */
    val rotationAvailable: Boolean
        get() = kitchens.count { it.rotationWeekPriceMinor != null } >= MealsViewModel.ROTATION_KITCHENS
}

/** What one storefront list was fetched for. */
data class StoreQuery(
    val at: LatLng,
    val slot: String?,
    val query: String,
    val cuisine: String?,
    val rotationOnly: Boolean,
) {
    companion object {
        fun of(s: StorefrontUiState) = s.at?.let { StoreQuery(it, s.slot, s.query.trim(), s.cuisine, s.rotationOnly) }
    }
}

data class CollectionUiState(
    val slug: String = "",
    val collection: CollectionKitchens? = null,
    val loading: Boolean = false,
    val error: Failure? = null,
) {
    /** The collection ended or never existed; retrying won't change that. */
    val gone: Boolean get() = error == Failure.NotFound
}

data class KitchenUiState(
    val branchId: String = "",
    val kitchen: KitchenDetail? = null,
    val reviews: List<KitchenReview> = emptyList(),
    /** Set when /reviews failed; the page keeps the kitchen and offers a retry for this section only. */
    val reviewsError: Failure? = null,
    /** Offers for plans (rotation-only ones dropped); empty when the call fails. */
    val offers: List<PromoOffer> = emptyList(),
    val loading: Boolean = false,
    val error: Failure? = null,
    /** The viewer's mark on this kitchen: favourite, avoid, or null (none, or signed out). */
    val taste: String? = null,
    val tasteBusy: Boolean = false,
    val tasteError: Failure? = null,
)

/** The renewal screen: the proposed four kitchens, edited, and the pay step. */
data class RenewUiState(
    val rotationId: String = "",
    val proposal: RenewalProposal? = null,
    /** Plan ids in the four slots, as the customer edited them. */
    val picks: List<String> = emptyList(),
    val loading: Boolean = false,
    val error: Failure? = null,
    /** Renewing, paying or changing the adventure setting. */
    val busy: Boolean = false,
    val actionError: Failure? = null,
    val paymentUrl: String? = null,
    /** Set once the next rotation exists; a retried pay pays it instead of renewing again. */
    val nextId: String? = null,
    val idempotencyKey: String = newIdempotencyKey(),
    val paid: Boolean = false,
) {
    /** The four kitchens as picked, with the proposal's reason where it proposed them. */
    val chosen: List<ProposalKitchen>
        get() {
            val p = proposal ?: return emptyList()
            return picks.mapNotNull { id -> p.kitchens.firstOrNull { it.planId == id } ?: p.eligible.firstOrNull { it.planId == id } }
        }
    val totalMinor: Long get() = chosen.sumOf { it.weekPriceMinor }
    val currency: String get() = proposal?.currency ?: "AED"
}

data class CheckoutUiState(
    val kitchen: KitchenDetail? = null,
    val plan: MealPlan? = null,
    val addresses: List<Address> = emptyList(),
    val addressId: String? = null,
    val weekdays: Set<Int> = emptySet(),
    val slots: Set<String> = emptySet(),
    val windowId: String? = null,
    val startDate: String = "",
    val promoCode: String = "",
    val preview: SubscriptionPreview? = null,
    val busy: Boolean = false,
    val error: Failure? = null,
    /** Bumped when a new address is saved, so the address form can close. */
    val addressAdded: Int = 0,
    /** Set once the order exists and needs paying on the provider's page. */
    val paymentUrl: String? = null,
    /** Set once paid (or once paying is handed to the browser); the screen moves on. */
    val subscriptionId: String? = null,
    /** The first start date whose cutoff is still ahead (YYYY-MM-DD, Asia/Dubai). */
    val earliestStart: String = "",
    /** The subscription created by this checkout and still unpaid; Pay retries only the payment. */
    val createdId: String? = null,
    /** The last payment attempt came back declined or did not reach the provider. */
    val paymentFailed: Boolean = false,
    /** The provider has not confirmed yet (requires_action with no page to open). */
    val paymentPending: Boolean = false,
    /** Each meal to its own address instead of every meal to [addressId]. */
    val perMeal: Boolean = false,
    /** Meal slot to address while [perMeal]; a slot not here goes to [addressId]. */
    val slotAddresses: Map<String, String> = emptyMap(),
    /** Addresses this kitchen does not deliver to (from the coverage check). */
    val outsideAddresses: Set<String> = emptySet(),
    /** Why the server refused a meal's address, by meal slot. */
    val slotProblems: Map<String, AddressProblem> = emptyMap(),
) {
    /** Two or more meals a day: they can go to different addresses. */
    val canSplitAddresses: Boolean get() = (plan?.mealsPerDay ?: 0) >= 2

    /** The chosen meals in order of the day. */
    val chosenSlots: List<String> get() = MealSlots.sorted(slots)

    /** Where [slot] goes as things stand. */
    fun addressFor(slot: String): String? = if (perMeal) slotAddresses[slot] ?: addressId else addressId

    /** The kitchen is known not to deliver to [id]. */
    fun outside(id: String): Boolean = id in outsideAddresses

    /** Chosen meals headed to an address the kitchen does not deliver to. */
    val uncoveredSlots: List<String>
        get() = chosenSlots.filter { slot -> addressFor(slot)?.let(::outside) == true }

    /** The one-address choice is an address the kitchen does not deliver to. */
    val addressOutside: Boolean get() = !perMeal && addressId?.let(::outside) == true

    val ready: Boolean
        get() = plan != null && addressId != null && windowId != null &&
            weekdays.size == plan.daysPerWeek && slots.size == plan.mealsPerDay &&
            validStart(startDate, earliestStart) && uncoveredSlots.isEmpty() &&
            (!perMeal || chosenSlots.all { addressFor(it) != null })
}

/** A YYYY-MM-DD date on or after [earliest]. */
internal fun validStart(date: String, earliest: String): Boolean {
    val d = runCatching { LocalDate.parse(date) }.getOrNull() ?: return false
    val e = runCatching { LocalDate.parse(earliest) }.getOrNull() ?: return true
    return d >= e
}

data class RotationUiState(
    val candidates: List<Kitchen> = emptyList(),
    val plans: Map<String, MealPlan> = emptyMap(),
    val picked: List<String> = emptyList(),
    val addresses: List<Address> = emptyList(),
    val addressId: String? = null,
    val startDate: String = "",
    val quote: Rotation? = null,
    val busy: Boolean = false,
    val error: Failure? = null,
    val paymentUrl: String? = null,
    /** Set once the rotation is created; a retried payment pays this one instead of creating another. */
    val rotationId: String? = null,
    /** The customer's adventure setting, shown in the builder; null until loaded (or signed out). */
    val adventure: String? = null,
    /** Sent with the create, one per draft, so a create retried after a timeout is not a second rotation. */
    val idempotencyKey: String = newIdempotencyKey(),
    val candidatesLoaded: Boolean = false,
    /** The rotation is created and its payment went through (or was handed to the provider). */
    val paid: Boolean = false,
    /** Days and meals a balanced suggestion chose; used while every pick still serves them. */
    val days: List<Int> = emptyList(),
    val slots: List<String> = emptyList(),
    val suggesting: Boolean = false,
    val suggestError: Failure? = null,
    /** Too few kitchens near the address for a balanced suggestion. */
    val suggestNone: Boolean = false,
    /** Each meal to its own address instead of every meal to [addressId]. */
    val perMeal: Boolean = false,
    /** Meal slot to address while [perMeal]; a slot not here goes to [addressId]. */
    val slotAddresses: Map<String, String> = emptyMap(),
    /** Address id to the candidate kitchens known not to deliver there. */
    val outside: Map<String, Set<String>> = emptyMap(),
) {
    /** Fewer kitchens deliver here than a rotation needs. */
    val tooFewKitchens: Boolean
        get() = candidatesLoaded && candidates.size < MealsViewModel.ROTATION_KITCHENS

    /**
     * The meals the per-meal picker offers: the ones every picked kitchen
     * serves, else the suggestion's, else any candidate's, in order of the day.
     */
    val mealSlotOptions: List<String>
        get() {
            val picks = picked.mapNotNull { plans[it] }
            return when {
                picks.isNotEmpty() -> MealSlots.sorted(picks.map { it.mealSlots.toSet() }.reduce { a, b -> a intersect b })
                slots.isNotEmpty() -> MealSlots.sorted(slots)
                else -> MealSlots.sorted(plans.values.flatMap { it.mealSlots })
            }
        }

    val canSplitAddresses: Boolean get() = mealSlotOptions.size >= 2

    fun addressFor(slot: String): String? = if (perMeal) slotAddresses[slot] ?: addressId else addressId

    /** Every address some meal goes to, the default first. */
    val chosenAddressIds: List<String>
        get() = if (!perMeal) listOfNotNull(addressId)
        else (mealSlotOptions.mapNotNull { addressFor(it) }.ifEmpty { listOfNotNull(addressId) }).distinct()

    /** The first chosen address [branchId] does not deliver to, or null when it covers them all (or is unknown). */
    fun unavailableAt(branchId: String): Address? =
        chosenAddressIds.firstOrNull { branchId in outside[it].orEmpty() }?.let { id -> addresses.firstOrNull { it.id == id } }

    fun unavailable(branchId: String): Boolean = unavailableAt(branchId) != null

    /** A picked kitchen does not deliver to one of the chosen addresses. */
    val pickedUnavailable: Boolean get() = picked.any(::unavailable)
}

@OptIn(ExperimentalUuidApi::class)
private fun newIdempotencyKey(): String = Uuid.random().toString()

data class MyMealsUiState(
    val subscriptions: List<MySubscription> = emptyList(),
    val rotations: List<Rotation> = emptyList(),
    /** Next week's kitchens, first delivery week first; the card shows the first. */
    val nextWeek: List<NextWeekPreview> = emptyList(),
    /** End-of-cycle recaps by rotation id, from week 4 on. */
    val recaps: Map<String, RotationRecap> = emptyMap(),
    /** Why the next-week card or recaps did not load; the list still shows. */
    val extrasError: Failure? = null,
    /** Active companies covering this person's office lunches. */
    val companies: List<ae.marketplace.mobile.model.CompanyBenefit> = emptyList(),
    val loading: Boolean = false,
    val loaded: Boolean = false,
    val error: Failure? = null,
    /** Rotations with a cancel, renewal or payment call in flight; their controls are disabled. */
    val busy: Set<String> = emptySet(),
    /** Why the last rotation action failed; shown over the loaded list. */
    val actionError: Failure? = null,
    val paymentUrl: String? = null,
    /** Cheaper kitchens for the rotation whose cancel sheet is open. */
    val cheaperFor: String? = null,
    val cheaper: CheaperPlans? = null,
    val cheaperLoading: Boolean = false,
    val cheaperError: Failure? = null,
)

/**
 * The change-address sheet on a subscription or rotation. [picks] holds the
 * address chosen per meal; only meals whose pick differs from where they go
 * now are sent. [changed] is filled after a save, for the confirmation.
 */
data class AddressChangeUiState(
    /** The sheet is showing ("open" is a Swift keyword). */
    val visible: Boolean = false,
    val addresses: List<Address> = emptyList(),
    val loading: Boolean = false,
    val loadError: Failure? = null,
    /** All meals to one address (true) or a choice per meal. */
    val allMeals: Boolean = true,
    val picks: Map<String, String> = emptyMap(),
    /**
     * Addresses one of the plan's kitchens does not deliver to, each with the
     * branch of the first kitchen that refuses it (a rotation checks every
     * remaining week's kitchen). The server still checks everything on save.
     */
    val outsideBy: Map<String, String> = emptyMap(),
    val busy: Boolean = false,
    val error: Failure? = null,
    /** Bumped each time a new address is saved from the sheet, so the form can close and clear. */
    val addressAdded: Int = 0,
    /** Why the new address could not be saved (the save error is [error]). */
    val addError: Failure? = null,
    val changed: List<DeliveryAddress> = emptyList(),
) {
    val outsideAddresses: Set<String> get() = outsideBy.keys
    fun picked(slot: String): String? = picks[slot]
    fun outside(id: String): Boolean = id in outsideBy
    /** The kitchen (branch id) known not to deliver to [id], when there is one. */
    fun outsideKitchen(id: String): String? = outsideBy[id]
    /** Why the server refused the change, worded by the screen. */
    val problem: AddressProblem? get() = AddressProblem.ofChange(error)
}

/** The meals [picks] moves away from where [current] sends them. */
internal fun addressMoves(current: List<DeliveryAddress>, picks: Map<String, String>): List<SlotAddress> =
    current.mapNotNull { d -> picks[d.mealSlot]?.takeIf { it != d.addressId }?.let { SlotAddress(d.mealSlot, it) } }

data class SubscriptionUiState(
    /** The subscription being shown, kept so a failed open can be retried. */
    val id: String? = null,
    val subscription: MySubscription? = null,
    val loading: Boolean = false,
    val busy: Boolean = false,
    val error: Failure? = null,
    val paymentUrl: String? = null,
    /** Set after a resume: the day deliveries restart. */
    val resumesOn: String? = null,
    val review: ReviewUiState = ReviewUiState(),
    /** Dishes the customer marked "not again"; the default moves off them. */
    val avoidedDishes: Set<String> = emptySet(),
    /** Dishes the customer hearted; a dish is never in both sets. */
    val lovedDishes: Set<String> = emptySet(),
    /** Dish marks being saved. */
    val tasteBusy: Set<String> = emptySet(),
    val tasteError: Failure? = null,
    /** Cheaper kitchens nearby, loaded for the "too expensive" cancel path. */
    val cheaper: CheaperPlans? = null,
    val cheaperLoading: Boolean = false,
    val cheaperError: Failure? = null,
    /** Set once a guest week was started from this screen. */
    val guestStarted: GuestWeekResult? = null,
    val addressChange: AddressChangeUiState = AddressChangeUiState(),
)

/**
 * One rotation: its four weeks, and the kitchens one week could change to
 * while [picking] is set. [busy] covers every call so a second tap is ignored.
 */
data class RotationDetailUiState(
    /** The rotation being shown, kept so a failed open can be retried. */
    val id: String? = null,
    val rotation: Rotation? = null,
    val loading: Boolean = false,
    val error: Failure? = null,
    val busy: Boolean = false,
    val actionError: Failure? = null,
    /** The week whose kitchen is being changed, with its options once loaded. */
    val picking: Int? = null,
    val options: RotationSwapOptions? = null,
    val optionsLoading: Boolean = false,
    val optionsError: Failure? = null,
    /** Set when a dearer kitchen needs the difference paid on the provider's page. */
    val paymentUrl: String? = null,
    /** Set once the rotation is cancelled, for the screen to leave. */
    val cancelled: Boolean = false,
    val addressChange: AddressChangeUiState = AddressChangeUiState(),
)

data class ReviewUiState(
    val data: MyReview? = null,
    val loading: Boolean = false,
    val saving: Boolean = false,
    val saved: Boolean = false,
    val error: Failure? = null,
)

/**
 * The meals tab: storefront, kitchen, checkout, rotation and the customer's
 * subscriptions. One holder for the tab, like MeViewModel, so going from a
 * kitchen to checkout and back keeps what was loaded.
 */
@OptIn(ExperimentalTime::class, ExperimentalUuidApi::class)
class MealsViewModel(private val api: MealsApi) : ViewModel() {

    private val _store = MutableStateFlow(StorefrontUiState())
    val store: StateFlow<StorefrontUiState> = _store.asStateFlow()

    private val _kitchen = MutableStateFlow(KitchenUiState())
    val kitchen: StateFlow<KitchenUiState> = _kitchen.asStateFlow()

    private val _checkout = MutableStateFlow(CheckoutUiState())
    val checkout: StateFlow<CheckoutUiState> = _checkout.asStateFlow()

    private val _rotation = MutableStateFlow(RotationUiState())
    val rotation: StateFlow<RotationUiState> = _rotation.asStateFlow()
    private val _renew = MutableStateFlow(RenewUiState())
    val renew: StateFlow<RenewUiState> = _renew.asStateFlow()

    private val _mine = MutableStateFlow(MyMealsUiState())
    val mine: StateFlow<MyMealsUiState> = _mine.asStateFlow()

    private val _sub = MutableStateFlow(SubscriptionUiState())
    val sub: StateFlow<SubscriptionUiState> = _sub.asStateFlow()

    private val _rotationDetail = MutableStateFlow(RotationDetailUiState())
    val rotationDetail: StateFlow<RotationDetailUiState> = _rotationDetail.asStateFlow()

    private val _collection = MutableStateFlow(CollectionUiState())
    val collection: StateFlow<CollectionUiState> = _collection.asStateFlow()

    private var storeJob: Job? = null
    private var previewJob: Job? = null

    // One Idempotency-Key per checkout draft: a retry of the same draft is the
    // same request to the server; any change to the draft gets a new key.
    private var draft: SubscribeInput? = null
    private var draftKey: String = ""
    private var createdOrderId: String? = null

    // ── Storefront ──────────────────────────────────────────────────────────

    fun setLocation(lat: Double, lng: Double) {
        val at = LatLng(lat, lng)
        if (_store.value.at == at) return
        _store.update { it.copy(at = at) }
        loadStore()
    }

    fun selectSlot(slot: String?) {
        _store.update { it.copy(slot = slot) }
        loadStore()
    }

    /** Search as you type: the list follows once typing pauses. */
    fun setQuery(query: String) {
        if (_store.value.query == query) return
        _store.update { it.copy(query = query) }
        load(debounceMillis = 300)
    }

    fun selectCuisine(cuisine: String?) {
        _store.update { it.copy(cuisine = cuisine) }
        loadStore()
    }

    fun setRotationOnly(on: Boolean) {
        _store.update { it.copy(rotationOnly = on) }
        loadStore()
    }

    fun clearFilters() {
        _store.update { it.copy(slot = null, query = "", cuisine = null, rotationOnly = false) }
        loadStore()
    }

    /**
     * Kitchens are the page, so only their failure fails it. Merchandising and
     * tomorrow's dishes are best-effort extras fetched alongside: when they
     * fail the plain list still shows, as on web.
     */
    fun loadStore() = load(debounceMillis = 0)

    private fun load(debounceMillis: Long) {
        val s0 = _store.value
        val key = StoreQuery.of(s0) ?: return
        val at = key.at
        storeJob?.cancel()
        storeJob = viewModelScope.launch {
            if (debounceMillis > 0) delay(debounceMillis)
            _store.update { it.copy(loading = true, error = null) }
            val unfiltered = !s0.filtering
            // Rails and banners are hidden while filtering, but the cuisine strip
            // stays, so sections are kept per place rather than per filter.
            val needHome = unfiltered || s0.loadedFor?.at != at
            try {
                coroutineScope {
                    val home: Deferred<List<HomeSection>?>? = if (needHome) {
                        async {
                            orNullUnlessCancelled {
                                api.home(at).sections.orEmpty().filter { it.enabled && it.type != "all_kitchens" }
                            }
                        }
                    } else {
                        null
                    }
                    val dishes: Deferred<List<DayDish>>? = if (unfiltered) {
                        async { orNullUnlessCancelled { api.tomorrowDishes(at).items } ?: emptyList() }
                    } else {
                        null
                    }
                    val kitchens = api.kitchens(at, key.slot, key.rotationOnly, key.query, key.cuisine).items
                    val sections = home?.await()
                    val day = dishes?.await()
                    _store.update {
                        val samePlace = it.loadedFor?.at == at
                        it.copy(
                            sections = sections ?: if (samePlace) it.sections else emptyList(),
                            kitchens = kitchens,
                            dishes = day ?: if (samePlace) it.dishes else emptyList(),
                            unfilteredCount = if (unfiltered) kitchens.size else if (samePlace) it.unfilteredCount else 0,
                            loadedFor = key,
                            loading = false,
                        )
                    }
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                val failure = e.toFailure()
                _store.update {
                    // A list for another place or filter must not pass as the
                    // answer for this one: drop it, keep only a same-query retry.
                    if (it.loadedFor == key) {
                        it.copy(loading = false, error = failure)
                    } else {
                        it.copy(
                            loading = false,
                            error = failure,
                            kitchens = emptyList(),
                            sections = if (it.loadedFor?.at == at) it.sections else emptyList(),
                            dishes = emptyList(),
                            loadedFor = null,
                        )
                    }
                }
            }
        }
    }

    /** A merchandised collection: only its kitchens that deliver to the current point. */
    fun openCollection(slug: String) {
        val at = _store.value.at ?: return
        _collection.value = CollectionUiState(slug = slug, loading = true)
        viewModelScope.launch {
            try {
                val c = api.collection(slug, at)
                _collection.update { if (it.slug == slug) it.copy(collection = c, loading = false) else it }
            } catch (e: Throwable) {
                val failure = e.toFailure()
                _collection.update { if (it.slug == slug) it.copy(loading = false, error = failure) else it }
            }
        }
    }

    fun retryCollection() {
        _collection.value.slug.takeIf { it.isNotEmpty() }?.let(::openCollection)
    }

    // ── Kitchen ─────────────────────────────────────────────────────────────

    fun openKitchen(branchId: String) {
        val at = _store.value.at ?: return
        _kitchen.value = KitchenUiState(branchId = branchId, loading = true)
        viewModelScope.launch {
            try {
                val k = api.kitchen(branchId, at)
                _kitchen.update { it.copy(kitchen = k, loading = false) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _kitchen.update { it.copy(loading = false, error = e.toFailure()) }
                return@launch
            }
            launch { loadOffers(branchId) }
            launch { loadKitchenTaste(branchId) }
            loadReviews(branchId)
        }
    }

    // ── Taste memory ────────────────────────────────────────────────────────

    // A mark is an extra: signed out or offline it is simply not shown.
    private suspend fun loadKitchenTaste(branchId: String) {
        val mark = runCatching { api.tastes().items }.getOrNull()
            ?.firstOrNull { it.subjectKind == "kitchen" && it.subjectId == branchId }?.sentiment
        _kitchen.update { if (it.branchId == branchId) it.copy(taste = mark) else it }
    }

    /** Hearts the open kitchen, marks it "not again", or (null) forgets the mark. Flips at once, back on failure. */
    fun setKitchenTaste(sentiment: String?) {
        val s = _kitchen.value
        val branchId = s.branchId.ifEmpty { return }
        if (s.tasteBusy) return
        val before = s.taste
        _kitchen.update { it.copy(taste = sentiment, tasteBusy = true, tasteError = null) }
        viewModelScope.launch {
            try {
                if (sentiment == null) api.clearTaste("kitchen", branchId, newIdempotencyKey()) else api.setTaste("kitchen", branchId, sentiment, newIdempotencyKey())
                _kitchen.update { if (it.branchId == branchId) it.copy(tasteBusy = false) else it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _kitchen.update { if (it.branchId == branchId) it.copy(taste = before, tasteBusy = false, tasteError = e.toFailure()) else it }
            }
        }
    }

    private suspend fun loadAvoidedDishes(subscriptionId: String) {
        val dishes = runCatching { api.tastes().items }.getOrNull()?.filter { it.subjectKind == "dish" } ?: return
        val avoided = dishes.filter { it.sentiment == "avoid" }.map { it.subjectId }.toSet()
        val loved = dishes.filter { it.sentiment == "favourite" }.map { it.subjectId }.toSet()
        _sub.update { if (it.id == subscriptionId) it.copy(avoidedDishes = avoided, lovedDishes = loved) else it }
    }

    /** "Not again" on a delivered dish (or taking it back). Flips at once, back on failure. */
    fun setDishAvoided(dishId: String, avoid: Boolean) = setDishTaste(dishId, if (avoid) "avoid" else null)

    /** "Loved it" on a delivered dish (or taking it back). Flips at once, back on failure. */
    fun setDishLoved(dishId: String, love: Boolean) = setDishTaste(dishId, if (love) "favourite" else null)

    /**
     * One mark per dish: the API keeps a single sentiment, so hearting an
     * avoided dish replaces the avoid, and the other way round. Null clears.
     */
    private fun setDishTaste(dishId: String, sentiment: String?) {
        val subId = _sub.value.id ?: return
        if (dishId in _sub.value.tasteBusy) return
        val before = _sub.value
        fun put(mark: String?) = _sub.update {
            it.copy(
                avoidedDishes = if (mark == "avoid") it.avoidedDishes + dishId else it.avoidedDishes - dishId,
                lovedDishes = if (mark == "favourite") it.lovedDishes + dishId else it.lovedDishes - dishId,
            )
        }
        put(sentiment)
        _sub.update { it.copy(tasteBusy = it.tasteBusy + dishId, tasteError = null) }
        viewModelScope.launch {
            try {
                if (sentiment != null) api.setTaste("dish", dishId, sentiment, newIdempotencyKey()) else api.clearTaste("dish", dishId, newIdempotencyKey())
                // The default dish moves off an avoided one: show the meals again.
                reloadSubscription(subId)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                put(
                    when (dishId) {
                        in before.avoidedDishes -> "avoid"
                        in before.lovedDishes -> "favourite"
                        else -> null
                    },
                )
                _sub.update { it.copy(tasteError = e.toFailure()) }
            } finally {
                _sub.update { it.copy(tasteBusy = it.tasteBusy - dishId) }
            }
        }
    }

    fun dismissTasteError() {
        _sub.update { it.copy(tasteError = null) }
        _kitchen.update { it.copy(tasteError = null) }
    }

    // ── Renewal with fresh kitchens ─────────────────────────────────────────

    /** Opens the renewal screen for a rotation: loads the proposal, picks start as proposed. */
    fun openRenewal(id: String) {
        _renew.value = RenewUiState(rotationId = id, loading = true)
        viewModelScope.launch { loadProposal(id) }
    }

    fun retryRenewal() {
        val id = _renew.value.rotationId.ifEmpty { return }
        _renew.update { it.copy(loading = true, error = null) }
        viewModelScope.launch { loadProposal(id) }
    }

    private suspend fun loadProposal(id: String) {
        try {
            val p = api.renewalProposal(id)
            _renew.update {
                if (it.rotationId != id) it
                else it.copy(proposal = p, picks = p.kitchens.map { k -> k.planId }, loading = false, error = null)
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            _renew.update { if (it.rotationId == id) it.copy(loading = false, error = e.toFailure()) else it }
        }
    }

    /** Puts another eligible kitchen in one of the four slots. A kitchen already picked stays where it is. */
    fun swapKitchen(slot: Int, planId: String) {
        _renew.update { s ->
            if (slot !in s.picks.indices || planId in s.picks || s.nextId != null) s
            else s.copy(picks = s.picks.toMutableList().also { it[slot] = planId }, actionError = null)
        }
    }

    /** Changes the adventure setting and asks for a fresh proposal. */
    fun setRenewalAdventure(adventure: String) {
        val id = _renew.value.rotationId.ifEmpty { return }
        if (_renew.value.busy) return
        _renew.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            try {
                api.setMealPreferences(adventure, newIdempotencyKey())
                _rotation.update { it.copy(adventure = adventure) }
                loadProposal(id)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _renew.update { it.copy(actionError = e.toFailure()) }
            } finally {
                _renew.update { it.copy(busy = false) }
            }
        }
    }

    /** Renews with the four picked plans and pays; a retry after a failed payment pays the same rotation. */
    fun confirmRenewal() {
        val s = _renew.value
        if (s.busy || s.picks.size != ROTATION_KITCHENS) return
        _renew.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            try {
                val nextId = s.nextId ?: api.renewRotation(s.rotationId, s.picks, s.idempotencyKey).id
                _renew.update { it.copy(nextId = nextId) }
                _mine.update { it.copy(loaded = false) }
                val intent = api.payRotation(nextId)
                _renew.update { it.copy(busy = false, paymentUrl = intent.redirectUrl, paid = true) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _renew.update { it.copy(busy = false, actionError = e.toFailure()) }
            }
        }
    }

    fun renewalPaymentOpened() = _renew.update { it.copy(paymentUrl = null) }

    fun dismissRenewalError() = _renew.update { it.copy(actionError = null) }

    fun retryReviews() {
        val branchId = _kitchen.value.branchId.ifEmpty { return }
        viewModelScope.launch { loadReviews(branchId) }
    }

    private suspend fun loadReviews(branchId: String) {
        _kitchen.update { it.copy(reviewsError = null) }
        try {
            val items = api.reviews(branchId).items
            _kitchen.update { if (it.branchId == branchId) it.copy(reviews = items) else it }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            _kitchen.update { if (it.branchId == branchId) it.copy(reviewsError = e.toFailure()) else it }
        }
    }

    // Offers are an extra on the page: a failure hides them rather than blocking it.
    private suspend fun loadOffers(branchId: String) {
        try {
            val items = api.offers(branchId).items.filter { it.appliesTo != "rotations" }
            _kitchen.update { if (it.branchId == branchId) it.copy(offers = items) else it }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Throwable) {
        }
    }

    // ── Checkout ────────────────────────────────────────────────────────────

    fun startCheckout(plan: MealPlan) {
        val k = _kitchen.value.kitchen ?: return
        val earliest = earliestStart(plan.cutoffHours).toString()
        draft = null
        createdOrderId = null
        _checkout.value = CheckoutUiState(
            earliestStart = earliest,
            kitchen = k,
            plan = plan,
            weekdays = plan.deliveryWeekdays.take(plan.daysPerWeek).toSet(),
            slots = plan.mealSlots.take(plan.mealsPerDay).toSet(),
            windowId = k.deliveryWindows.firstOrNull()?.id,
            startDate = earliest,
        )
        loadAddresses()
    }

    /** Retry after a failed address load or preview. */
    fun retryCheckout() {
        if (_checkout.value.addresses.isEmpty()) loadAddresses() else refreshPreview()
    }

    private fun loadAddresses() {
        viewModelScope.launch {
            try {
                val items = api.addresses().items
                val pick = items.firstOrNull { it.isDefault } ?: items.firstOrNull()
                _checkout.update { it.copy(addresses = items, addressId = it.addressId ?: pick?.id) }
                _rotation.update { it.copy(addresses = items, addressId = it.addressId ?: pick?.id) }
                refreshPreview()
                checkCheckoutCoverage()
                checkRotationCoverage()
            } catch (e: Throwable) {
                _checkout.update { it.copy(error = e.toFailure()) }
            }
        }
    }

    /** Saved addresses for the storefront's location picker; quietly empty when signed out. */
    fun loadPlaceAddresses() {
        viewModelScope.launch {
            val items = runCatching { api.addresses().items }.getOrDefault(emptyList())
            _store.update { it.copy(addresses = items) }
        }
    }

    /**
     * Saves what the customer typed, as typed: label and city are required by
     * the server and its 422 comes back as field errors, so a blank is never
     * silently stored as "Home" or "Dubai". [at] must be a pin the customer
     * chose (a fresh fix), not the feed's area or a fallback point.
     */
    fun addAddress(label: String, building: String, area: String, city: String, at: LatLng) {
        if (_checkout.value.busy) return
        viewModelScope.launch {
            _checkout.update { it.copy(busy = true, error = null) }
            try {
                val a = api.createAddress(
                    AddressInput(
                        label = label.trim(),
                        building = building.trim().ifBlank { null },
                        area = area.trim().ifBlank { null },
                        city = city.trim(),
                        point = at,
                        isDefault = _checkout.value.addresses.isEmpty(),
                    ),
                )
                _checkout.update {
                    it.copy(addresses = it.addresses + a, addressId = a.id, busy = false, addressAdded = it.addressAdded + 1)
                }
                _rotation.update { it.copy(addresses = it.addresses + a, addressId = a.id) }
                _checkout.value.let { c ->
                    if (c.perMeal) {
                        // A new address saved mid per-meal choice goes to the first meal still headed nowhere it can.
                        val target = c.chosenSlots.firstOrNull { slot -> c.addressFor(slot)?.let(c::outside) == true }
                        if (target != null) _checkout.update { it.copy(slotAddresses = it.slotAddresses + (target to a.id)) }
                    }
                }
                refreshPreview()
                checkCheckoutCoverage()
                checkRotationCoverage()
            } catch (e: Throwable) {
                _checkout.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
    }

    fun selectAddress(id: String) {
        _checkout.update { it.copy(addressId = id, slotProblems = emptyMap()) }
        refreshPreview()
    }

    /** Every meal to one address ([on] false) or a picker per meal, starting from the one address. */
    fun setPerMealAddresses(on: Boolean) {
        _checkout.update { s ->
            if (!s.canSplitAddresses && on) return
            val start = s.addressId
            s.copy(
                perMeal = on,
                slotAddresses = if (on && start != null) s.plan?.mealSlots.orEmpty().associateWith { slot -> s.slotAddresses[slot] ?: start } else s.slotAddresses,
                slotProblems = emptyMap(),
            )
        }
        refreshPreview()
    }

    /** Sends [slot] ("lunch") to address [id]. */
    fun selectSlotAddress(slot: String, id: String) {
        _checkout.update { it.copy(perMeal = true, slotAddresses = it.slotAddresses + (slot to id), slotProblems = it.slotProblems - slot) }
        refreshPreview()
    }

    /** Asks which saved addresses this kitchen delivers to; unknown stays unknown (the server still checks). */
    private fun checkCheckoutCoverage() {
        val s = _checkout.value
        val branch = s.kitchen?.branchId ?: return
        val ids = s.addresses.map { it.id }
        if (ids.isEmpty()) return
        viewModelScope.launch {
            val cov = orNullUnlessCancelled { api.addressCoverage(listOf(branch), ids) } ?: return@launch
            val outside = cov.items.filter { !it.delivers && it.branchId == branch }.map { it.addressId }.toSet()
            val known = cov.items.map { it.addressId }.toSet()
            _checkout.update { if (it.kitchen?.branchId == branch) it.copy(outsideAddresses = it.outsideAddresses - known + outside) else it }
        }
    }

    fun toggleWeekday(day: Int) {
        _checkout.update { s ->
            val max = s.plan?.daysPerWeek ?: return
            val next = if (day in s.weekdays) s.weekdays - day
            else if (s.weekdays.size < max) s.weekdays + day else s.weekdays
            s.copy(weekdays = next)
        }
        refreshPreview()
    }

    fun toggleSlot(slot: String) {
        _checkout.update { s ->
            val max = s.plan?.mealsPerDay ?: return
            val next = if (slot in s.slots) s.slots - slot
            else if (s.slots.size < max) s.slots + slot else s.slots
            s.copy(slots = next)
        }
        refreshPreview()
    }

    fun selectWindow(id: String) { _checkout.update { it.copy(windowId = id) }; refreshPreview() }

    fun setStartDate(date: String) { _checkout.update { it.copy(startDate = date) }; refreshPreview() }

    fun setPromo(code: String) { _checkout.update { it.copy(promoCode = code.uppercase()) } }

    fun applyPromo() = refreshPreview()

    private fun subscribeInput(s: CheckoutUiState): SubscribeInput? {
        if (!s.ready) return null
        val window = s.windowId ?: return null
        val slots = s.chosenSlots
        // Per meal, the first meal's address is the plan's default and every
        // rule names its own, so a refusal points at the meal it is about.
        val default = if (s.perMeal) slots.firstNotNullOfOrNull { s.addressFor(it) } ?: s.addressId!! else s.addressId!!
        return SubscribeInput(
            planId = s.plan!!.id,
            addressId = default,
            startDate = s.startDate,
            schedule = s.weekdays.sorted().flatMap { d ->
                slots.map { ScheduleRule(d, it, window, addressId = if (s.perMeal) s.addressFor(it) else null) }
            },
            promoCode = s.promoCode.ifBlank { null },
        )
    }

    /** The checkout failure with any per-meal address refusals pulled out by meal. */
    private fun CheckoutUiState.failed(e: Throwable, input: SubscribeInput): CheckoutUiState {
        val f = e.toFailure()
        return copy(error = f, slotProblems = AddressProblem.bySlot(f, input.schedule, input.addressId, perMeal))
    }

    private fun refreshPreview() {
        val input = subscribeInput(_checkout.value) ?: run {
            _checkout.update { it.copy(preview = null) }
            return
        }
        previewJob?.cancel()
        previewJob = viewModelScope.launch {
            try {
                val p = api.preview(input)
                _checkout.update { it.copy(preview = p, error = null, slotProblems = emptyMap()) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _checkout.update { it.copy(preview = null).failed(e, input) }
            }
        }
    }

    /**
     * Creates the subscription and pays its first order.
     *
     * Two steps that fail separately. Creating is keyed per draft, so pressing
     * Pay again after a timeout returns the same subscription. Once it exists,
     * a failed payment only retries the payment, never the signup.
     */
    fun subscribe() {
        if (_checkout.value.busy) return
        val input = subscribeInput(_checkout.value) ?: return
        if (input != draft) {
            draft = input
            draftKey = Uuid.random().toString()
            createdOrderId = null
            _checkout.update { it.copy(createdId = null) }
        }
        val key = draftKey
        viewModelScope.launch {
            _checkout.update { it.copy(busy = true, error = null, paymentFailed = false, paymentPending = false) }
            val subId: String
            val orderId: String
            try {
                val existing = createdOrderId
                if (existing != null && _checkout.value.createdId != null) {
                    subId = _checkout.value.createdId!!
                    orderId = existing
                } else {
                    val created = api.subscribe(input, key)
                    subId = created.id
                    orderId = created.orderId
                    createdOrderId = orderId
                    _checkout.update { it.copy(createdId = subId) }
                    _mine.update { it.copy(loaded = false) }
                }
            } catch (e: Throwable) {
                _checkout.update { it.copy(busy = false).failed(e, input) }
                return@launch
            }
            try {
                val intent = api.payOrder(orderId)
                _checkout.update {
                    when {
                        intent.redirectUrl != null ->
                            it.copy(busy = false, paymentUrl = intent.redirectUrl, subscriptionId = subId)
                        intent.status == "authorized" ->
                            it.copy(busy = false, subscriptionId = subId)
                        intent.status == "requires_action" ->
                            it.copy(busy = false, paymentPending = true, subscriptionId = subId)
                        else -> it.copy(busy = false, paymentFailed = true)
                    }
                }
                _mine.update { it.copy(loaded = false) }
            } catch (e: Throwable) {
                _checkout.update { it.copy(busy = false, paymentFailed = true, error = e.toFailure()) }
            }
        }
    }

    fun paymentOpened() {
        _checkout.update { it.copy(paymentUrl = null) }
        _rotation.update { it.copy(paymentUrl = null) }
        _sub.update { it.copy(paymentUrl = null) }
        _mine.update { it.copy(paymentUrl = null) }
    }

    // ── Rotation ────────────────────────────────────────────────────────────

    fun startRotation() {
        val at = _store.value.at ?: return
        _rotation.value = RotationUiState(busy = true, startDate = earliestStart(48).toString())
        viewModelScope.launch {
            launch {
                val a = runCatching { api.mealPreferences().adventure }.getOrNull()
                _rotation.update { it.copy(adventure = a) }
            }
            try {
                val kitchens = api.kitchens(at, rotation = true).items
                // One kitchen that fails to load is left out rather than failing the list.
                val found = coroutineScope {
                    kitchens.map { k ->
                        async {
                            try {
                                Result.success(api.kitchen(k.branchId, at).plans.firstOrNull { it.rotationEnabled }?.let { k.branchId to it })
                            } catch (e: CancellationException) {
                                throw e
                            } catch (e: Throwable) {
                                Result.failure(e)
                            }
                        }
                    }.awaitAll()
                }
                if (kitchens.isNotEmpty() && found.all { it.isFailure }) throw found.first().exceptionOrNull()!!
                val plans = found.mapNotNull { it.getOrNull() }.toMap()
                _rotation.update {
                    it.copy(
                        candidates = kitchens.filter { k -> k.branchId in plans },
                        plans = plans,
                        busy = false,
                        candidatesLoaded = true,
                    )
                }
                checkRotationCoverage()
            } catch (e: Throwable) {
                _rotation.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
        loadAddresses()
    }

    /** The adventure setting from the builder; the builder itself is unaffected, renewals follow it. */
    fun setBuilderAdventure(adventure: String) {
        val before = _rotation.value.adventure
        _rotation.update { it.copy(adventure = adventure) }
        viewModelScope.launch {
            try {
                api.setMealPreferences(adventure, newIdempotencyKey())
            } catch (e: CancellationException) {
                throw e
            } catch (_: Throwable) {
                _rotation.update { it.copy(adventure = before) }
            }
        }
    }

    fun togglePick(branchId: String) {
        _rotation.update { s ->
            // A kitchen that does not deliver to a chosen address can be let go, not picked.
            if (branchId !in s.picked && s.unavailable(branchId)) return
            val next = if (branchId in s.picked) s.picked - branchId
            else if (s.picked.size < ROTATION_KITCHENS) s.picked + branchId else s.picked
            // The server refuses a start before the slowest picked kitchen's
            // cutoff, so the date keeps up with the picks.
            val cutoff = next.mapNotNull { s.plans[it]?.cutoffHours }.maxOrNull() ?: 48
            s.copy(picked = next, startDate = maxOf(s.startDate, earliestStart(cutoff).toString()), quote = null).newDraft()
        }
        quoteRotation()
    }

    fun selectRotationAddress(id: String) {
        _rotation.update { it.copy(addressId = id, quote = null, error = null).newDraft() }
        checkRotationCoverage()
        quoteRotation()
    }

    /** Every meal of the rotation to one address, or a picker per meal starting from it. */
    fun setRotationPerMeal(on: Boolean) {
        _rotation.update { s ->
            if (on && !s.canSplitAddresses) return
            val start = s.addressId
            s.copy(
                perMeal = on,
                slotAddresses = if (on && start != null) s.mealSlotOptions.associateWith { slot -> s.slotAddresses[slot] ?: start } else s.slotAddresses,
                quote = null,
                error = null,
            ).newDraft()
        }
        checkRotationCoverage()
        quoteRotation()
    }

    fun selectRotationSlotAddress(slot: String, id: String) {
        _rotation.update { it.copy(perMeal = true, slotAddresses = it.slotAddresses + (slot to id), quote = null, error = null).newDraft() }
        checkRotationCoverage()
        quoteRotation()
    }

    /** Marks the candidate kitchens that do not deliver to a chosen address not checked yet. */
    private fun checkRotationCoverage() {
        val s = _rotation.value
        val branches = s.candidates.map { it.branchId }
        val missing = s.chosenAddressIds.filter { it !in s.outside }
        if (branches.isEmpty() || missing.isEmpty()) return
        viewModelScope.launch {
            val cov = orNullUnlessCancelled { api.addressCoverage(branches, missing) } ?: return@launch
            val answered = cov.items.map { it.addressId }.toSet()
            val byAddress = answered.associateWith { a -> cov.items.filter { it.addressId == a && !it.delivers }.map { it.branchId }.toSet() }
            _rotation.update { it.copy(outside = it.outside + byAddress) }
        }
    }

    /** A changed pick is a different rotation: it gets its own create. */
    private fun RotationUiState.newDraft(): RotationUiState = copy(rotationId = null, idempotencyKey = newIdempotencyKey())

    private fun rotationInput(s: RotationUiState): RotationInput? {
        if (s.picked.size != ROTATION_KITCHENS) return null
        val address = s.addressId ?: return null
        val plans = s.picked.mapNotNull { s.plans[it] }
        if (plans.size != ROTATION_KITCHENS) return null
        // The days and meals every picked kitchen serves.
        val days = plans.map { it.deliveryWeekdays.toSet() }.reduce { a, b -> a intersect b }
        val slots = plans.map { it.mealSlots.toSet() }.reduce { a, b -> a intersect b }
        val perWeek = plans.minOf { it.daysPerWeek }
        val perDay = plans.minOf { it.mealsPerDay }
        val chosenDays = s.days.takeIf { it.size == perWeek && days.containsAll(it) } ?: days.sorted().take(perWeek)
        val chosenSlots = s.slots.takeIf { it.size == perDay && slots.containsAll(it) } ?: slots.sorted().take(perDay)
        // Per meal, the first meal's address is the default; only meals going
        // elsewhere are listed, and only meals the rotation delivers.
        val default = if (s.perMeal) MealSlots.sorted(chosenSlots).firstNotNullOfOrNull { s.addressFor(it) } ?: address else address
        val perSlot = if (s.perMeal) chosenSlots.mapNotNull { slot -> s.addressFor(slot)?.takeIf { it != default }?.let { SlotAddress(slot, it) } } else emptyList()
        return RotationInput(
            planIds = plans.map { it.id },
            addressId = default,
            startDate = s.startDate,
            deliveryWeekdays = chosenDays.sorted(),
            mealSlots = chosenSlots,
            slotAddresses = perSlot.ifEmpty { null },
        )
    }

    /**
     * Asks for the price again after a quote failed: the pay button needs a
     * quote, so without this a dropped connection while pricing is a dead end.
     */
    fun retryQuote() {
        if (_rotation.value.busy) return
        _rotation.update { it.copy(error = null) }
        quoteRotation()
    }

    private fun quoteRotation() {
        val input = rotationInput(_rotation.value) ?: return
        viewModelScope.launch {
            try {
                val q = api.previewRotation(input)
                _rotation.update { it.copy(quote = q, error = null) }
            } catch (e: Throwable) {
                val f = e.toFailure()
                // The server is the authority on cutoffs: a start it refuses as
                // too soon moves to the first date it names, and the quote is
                // asked for again, instead of leaving the builder stuck.
                val earliest = earliestFromTooSoon(f)
                if (earliest != null && earliest > input.startDate) {
                    _rotation.update { it.copy(startDate = earliest, quote = null, error = null).newDraft() }
                    quoteRotation()
                } else {
                    _rotation.update { it.copy(error = f) }
                }
            }
        }
    }

    /** Fills the four picks from a balanced suggestion; the customer can still swap any of them. */
    fun suggestRotation() {
        val s = _rotation.value
        if (s.suggesting || s.busy) return
        val address = s.addressId ?: return
        _rotation.update { it.copy(suggesting = true, suggestError = null, suggestNone = false) }
        val options = s.mealSlotOptions
        val default = if (s.perMeal) options.firstNotNullOfOrNull { s.addressFor(it) } ?: address else address
        val perSlot = if (s.perMeal) options.mapNotNull { slot -> s.addressFor(slot)?.takeIf { it != default }?.let { SlotAddress(slot, it) } } else emptyList()
        viewModelScope.launch {
            try {
                val input = RotationSuggestionInput(default, slotAddresses = perSlot.ifEmpty { null })
                val sug = try {
                    api.suggestRotation(input)
                } catch (e: ApiException) {
                    // The suggestion picked meals without one the customer gave an
                    // address for: ask again for exactly the meals they set up.
                    if (perSlot.isNotEmpty() && e.problem?.errors.orEmpty().any { it.field == "slot_addresses" && it.code == "unknown_slot" }) {
                        api.suggestRotation(input.copy(mealSlots = options))
                    } else {
                        throw e
                    }
                }
                _rotation.update {
                    it.copy(
                        picked = sug.branchIds.filter { id -> id in it.plans }.take(ROTATION_KITCHENS),
                        days = sug.deliveryWeekdays,
                        slots = sug.mealSlots,
                        startDate = maxOf(it.startDate, sug.startDate),
                        quote = null,
                        suggesting = false,
                    ).newDraft()
                }
                quoteRotation()
            } catch (e: Throwable) {
                val f = e.toFailure()
                _rotation.update {
                    // Only "no kitchens to suggest" is the quiet empty state; a
                    // promo or address refusal is shown as the error it is.
                    if (f is Failure.Rejected && f.fieldCodes.containsValue("not_enough_kitchens")) {
                        it.copy(suggesting = false, suggestNone = true)
                    } else {
                        it.copy(suggesting = false, suggestError = f)
                    }
                }
            }
        }
    }

    fun payRotation() {
        val draft = _rotation.value
        if (draft.busy || draft.pickedUnavailable) return
        val input = rotationInput(draft) ?: return
        viewModelScope.launch {
            _rotation.update { it.copy(busy = true, error = null) }
            try {
                // Created on an earlier try whose payment failed: pay that one.
                val id = draft.rotationId ?: api.createRotation(input, draft.idempotencyKey).id
                _rotation.update { it.copy(rotationId = id) }
                _mine.update { it.copy(loaded = false) }
                val intent = api.payRotation(id)
                _rotation.update { it.copy(busy = false, paymentUrl = intent.redirectUrl, paid = true) }
            } catch (e: Throwable) {
                _rotation.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
    }

    // ── My meals ────────────────────────────────────────────────────────────

    fun loadMine(force: Boolean = false) {
        if (_mine.value.loaded && !force) return
        viewModelScope.launch {
            _mine.update { it.copy(loading = true, error = null) }
            try {
                fetchMine()
            } catch (e: Throwable) {
                _mine.update { it.copy(loading = false, error = e.toFailure()) }
            }
        }
    }

    private suspend fun fetchMine() {
        val subs = api.subscriptions().items
        val rotations = api.rotations().items
        // The office-lunch card is extra: its failure must not hide the list.
        val companies = runCatching { api.myCompanies().items }.getOrDefault(emptyList())
            .filter { c -> c.status == "active" }
        // The next-week card and recaps are extras too: a failure shows a Retry strip.
        var extrasError: Failure? = null
        val nextWeek = try { api.nextWeek().items } catch (e: Throwable) { extrasError = e.toFailure(); emptyList() }
        val recaps = if (rotations.isEmpty()) emptyList() else try {
            api.rotationRecaps().items
        } catch (e: Throwable) { extrasError = extrasError ?: e.toFailure(); emptyList() }
        _mine.update {
            it.copy(
                companies = companies,
                // Rotation weeks show under their rotation, not as four more rows.
                subscriptions = subs.filter { s -> s.rotationId == null },
                rotations = rotations,
                nextWeek = nextWeek,
                recaps = recaps.associateBy { r -> r.rotationId },
                extrasError = extrasError,
                loading = false,
                loaded = true,
            )
        }
    }

    private var openedId: String? = null

    /** Loads the subscription that failed to open again. */
    fun retryOpen() {
        openedId?.let(::openSubscription)
    }

    fun openSubscription(id: String) {
        openedId = id
        _sub.value = SubscriptionUiState(id = id, loading = true)
        lastAction = null
        reloadSubscription(id)
        loadReview(id)
    }

    private fun reloadSubscription(id: String) {
        viewModelScope.launch {
            launch { loadAvoidedDishes(id) }
            try {
                val s = api.subscription(id)
                _sub.update { it.copy(subscription = s, loading = false, busy = false, error = null) }
            } catch (e: Throwable) {
                _sub.update { it.copy(loading = false, busy = false, error = e.toFailure()) }
            }
        }
    }

    private var lastAction: (suspend (String) -> Unit)? = null

    private fun act(block: suspend (String) -> Unit) {
        val id = _sub.value.subscription?.id ?: return
        // One call at a time: a second tap while busy is ignored.
        if (_sub.value.busy) return
        lastAction = block
        _sub.update { it.copy(busy = true, error = null) }
        viewModelScope.launch {
            try {
                block(id)
                lastAction = null
                _mine.update { it.copy(loaded = false) }
                reloadSubscription(id)
            } catch (e: Throwable) {
                _sub.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
    }

    /** Tries the failed action again, or reloads when nothing failed mid-action. */
    fun retry() {
        val block = lastAction
        val id = _sub.value.subscription?.id ?: return
        if (block != null) act(block) else reloadSubscription(id)
    }

    fun dismissError() {
        lastAction = null
        _sub.update { it.copy(error = null) }
    }

    fun choose(date: String, slot: String, dishId: String) = act { api.choose(it, date, slot, dishId) }
    fun skip(date: String, slot: String) = act { api.skip(it, date, slot) }
    fun unskip(date: String, slot: String) = act { api.unskip(it, date, slot) }
    fun pause(from: String, to: String) = act { api.pause(it, from, to) }
    fun resume() = act { id ->
        val out = api.resume(id)
        _sub.update { it.copy(resumesOn = out.resumesOn) }
    }
    fun cancel() = act { api.cancel(it, "customer_request") }

    /** Cancel with one of CancelReasons and an optional note. */
    fun cancel(reasonCode: String?, note: String) = act { api.cancel(it, reasonCode, note.trim(), newIdempotencyKey()) }

    /** "Try a different kitchen next week": one of the offer's options. */
    fun startGuestWeek(planId: String) = act { id ->
        val out = api.startGuestWeek(id, planId, newIdempotencyKey())
        _sub.update { it.copy(guestStarted = out) }
        if (out.pendingOrderId != null) {
            api.payOrder(out.pendingOrderId).redirectUrl?.let { url -> _sub.update { it.copy(paymentUrl = url) } }
        }
    }

    fun dismissGuestWeek() = act { api.dismissGuestWeek(it, newIdempotencyKey()) }

    fun loadCheaperPlans() {
        val id = _sub.value.subscription?.id ?: return
        if (_sub.value.cheaperLoading) return
        _sub.update { it.copy(cheaperLoading = true, cheaperError = null) }
        viewModelScope.launch {
            try {
                val out = api.cheaperPlans(id)
                _sub.update { it.copy(cheaper = out, cheaperLoading = false) }
            } catch (e: Throwable) {
                _sub.update { it.copy(cheaperLoading = false, cheaperError = e.toFailure()) }
            }
        }
    }

    fun loadReview(id: String = _sub.value.subscription?.id ?: "") {
        if (id.isEmpty()) return
        _sub.update { it.copy(review = it.review.copy(loading = true, error = null)) }
        viewModelScope.launch {
            try {
                val r = api.myReview(id)
                _sub.update { it.copy(review = it.review.copy(data = r, loading = false)) }
            } catch (e: Throwable) {
                _sub.update { it.copy(review = it.review.copy(loading = false, error = e.toFailure())) }
            }
        }
    }

    fun saveReview(rating: Int, comment: String) {
        val id = _sub.value.subscription?.id ?: return
        if (_sub.value.review.saving || rating !in 1..5) return
        _sub.update { it.copy(review = it.review.copy(saving = true, saved = false, error = null)) }
        viewModelScope.launch {
            try {
                api.writeReview(id, rating, comment.trim())
                val r = api.myReview(id)
                _sub.update { it.copy(review = ReviewUiState(data = r, saved = true)) }
            } catch (e: Throwable) {
                _sub.update { it.copy(review = it.review.copy(saving = false, error = e.toFailure())) }
            }
        }
    }
    fun setAutoRenew(auto: Boolean) = act { api.setRenewal(it, auto) }

    /** Pays the outstanding order: a first cycle that failed, or a renewal that is due. */
    fun payPending() {
        val order = _sub.value.subscription?.pendingOrderId ?: return
        act { api.payOrder(order).redirectUrl?.let { url -> _sub.update { s -> s.copy(paymentUrl = url) } } }
    }

    fun renew() = act { id ->
        val next = api.renew(id, newIdempotencyKey())
        api.payOrder(next.orderId).redirectUrl?.let { url -> _sub.update { s -> s.copy(paymentUrl = url) } }
    }

    /** Runs one card action; the card's controls are disabled until it settles, then the list reloads. */
    private fun rotationAction(id: String, onFailure: () -> Unit = {}, block: suspend () -> Unit) {
        if (id in _mine.value.busy) return
        _mine.update { it.copy(busy = it.busy + id, actionError = null) }
        viewModelScope.launch {
            try {
                block()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                onFailure()
                _mine.update { it.copy(actionError = e.toFailure()) }
            } finally {
                _mine.update { it.copy(busy = it.busy - id) }
            }
            loadMine(force = true)
        }
    }

    fun dismissMineError() = _mine.update { it.copy(error = null) }

    private fun setLocalRenewal(id: String, auto: Boolean) = _mine.update { s ->
        s.copy(rotations = s.rotations.map { if (it.id == id) it.copy(autoRenew = auto) else it })
    }

    fun cancelRotation(id: String) = rotationAction(id) { api.cancelRotation(id, "customer_request") }

    private fun setLocalRefresh(id: String, on: Boolean) = _mine.update { s ->
        s.copy(rotations = s.rotations.map { if (it.id == id) it.copy(refreshOnRenew = on) else it })
    }

    /** Fresh kitchens at renewal, on or off. Flips the switch now and puts it back if the server refuses. */
    fun setRotationRefresh(id: String, on: Boolean) {
        if (id in _mine.value.busy) return
        setLocalRefresh(id, on)
        rotationAction(id, onFailure = { setLocalRefresh(id, !on) }) { api.setRotationRefresh(id, on) }
    }

    fun cancelRotation(id: String, reasonCode: String?, note: String) =
        rotationAction(id) { api.cancelRotation(id, reasonCode, note.trim(), newIdempotencyKey()) }

    fun loadRotationCheaperPlans(id: String) {
        if (_mine.value.cheaperLoading && _mine.value.cheaperFor == id) return
        _mine.update { it.copy(cheaperFor = id, cheaper = null, cheaperLoading = true, cheaperError = null) }
        viewModelScope.launch {
            try {
                val out = api.rotationCheaperPlans(id)
                _mine.update { if (it.cheaperFor == id) it.copy(cheaper = out, cheaperLoading = false) else it }
            } catch (e: Throwable) {
                _mine.update { if (it.cheaperFor == id) it.copy(cheaperLoading = false, cheaperError = e.toFailure()) else it }
            }
        }
    }

    /** Flips the switch now and puts it back if the server refuses. */
    fun setRotationRenewal(id: String, auto: Boolean) {
        if (id in _mine.value.busy) return
        setLocalRenewal(id, auto)
        rotationAction(id, onFailure = { setLocalRenewal(id, !auto) }) { api.setRotationRenewal(id, auto) }
    }

    /** Pays a rotation that is awaiting payment: one whose first payment failed, or a prepared renewal. */
    fun payPendingRotation(id: String) = rotationAction(id) {
        api.payRotation(id).redirectUrl?.let { url -> _mine.update { it.copy(paymentUrl = url) } }
    }

    /** Prepares the next rotation and pays for it; if paying fails it waits on its card with Pay now. */
    fun renewRotation(id: String) = rotationAction(id) {
        val next = api.renewRotation(id, idempotencyKey = newIdempotencyKey())
        api.payRotation(next.id).redirectUrl?.let { url -> _mine.update { it.copy(paymentUrl = url) } }
    }

    fun dismissRotationError() = _mine.update { it.copy(actionError = null) }

    // --- one rotation ---------------------------------------------------------

    fun openRotation(id: String) {
        _rotationDetail.value = RotationDetailUiState(id = id, loading = true)
        reloadRotation(id)
    }

    fun retryOpenRotation() {
        val id = _rotationDetail.value.id ?: return
        _rotationDetail.update { it.copy(loading = true, error = null) }
        reloadRotation(id)
    }

    private fun reloadRotation(id: String) {
        viewModelScope.launch {
            try {
                val r = api.rotation(id)
                _rotationDetail.update { it.copy(rotation = r, loading = false, error = null) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _rotationDetail.update { it.copy(loading = false, error = e.toFailure()) }
            }
        }
    }

    /** Opens the kitchen picker for [week] and loads what it could change to. */
    fun pickWeekKitchen(week: Int) {
        val id = _rotationDetail.value.id ?: return
        _rotationDetail.update { it.copy(picking = week, options = null, optionsLoading = true, optionsError = null, actionError = null) }
        loadSwapOptions(id, week)
    }

    fun retrySwapOptions() {
        val s = _rotationDetail.value
        val id = s.id ?: return
        val week = s.picking ?: return
        _rotationDetail.update { it.copy(optionsLoading = true, optionsError = null) }
        loadSwapOptions(id, week)
    }

    fun closeWeekPicker() = _rotationDetail.update { it.copy(picking = null, options = null, optionsError = null) }

    private fun loadSwapOptions(id: String, week: Int) {
        viewModelScope.launch {
            try {
                val o = api.rotationSwapOptions(id, week)
                _rotationDetail.update { if (it.picking == week) it.copy(options = o, optionsLoading = false) else it }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _rotationDetail.update { if (it.picking == week) it.copy(optionsLoading = false, optionsError = e.toFailure()) else it }
            }
        }
    }

    /**
     * Changes the picked week's kitchen to [planId]. A dearer kitchen hands
     * back the provider's page for the difference; the week shows as awaiting
     * payment until it is paid.
     */
    fun swapWeekKitchen(planId: String) {
        val s = _rotationDetail.value
        val id = s.id ?: return
        val week = s.picking ?: return
        rotationDetailAction(id) {
            val res = api.swapRotationWeek(id, week, planId, newIdempotencyKey())
            _rotationDetail.update {
                it.copy(rotation = res.rotation, picking = null, options = null, paymentUrl = res.payment?.redirectUrl)
            }
        }
    }

    /**
     * Retries the payment for a swapped-in week whose dearer kitchen's
     * difference was not collected: the same swap again returns the same
     * order and a fresh provider page. Nothing to do once it is paid.
     */
    fun payWeekDifference(week: Int) {
        val s = _rotationDetail.value
        val id = s.id ?: return
        val w = s.rotation?.weeks?.firstOrNull { it.week == week } ?: return
        if ((w.dueMinor ?: 0L) <= 0L) return
        rotationDetailAction(id) {
            val res = api.swapRotationWeek(id, week, w.planId, newIdempotencyKey())
            _rotationDetail.update { it.copy(rotation = res.rotation, paymentUrl = res.payment?.redirectUrl) }
        }
    }

    /** Cancels the open rotation; My meals reloads so its card agrees. */
    fun cancelOpenRotation(reasonCode: String?, note: String) {
        val id = _rotationDetail.value.id ?: return
        rotationDetailAction(id) {
            api.cancelRotation(id, reasonCode, note.trim(), newIdempotencyKey())
            _rotationDetail.update { it.copy(cancelled = true) }
        }
    }

    fun dismissRotationDetailError() = _rotationDetail.update { it.copy(actionError = null) }

    fun clearRotationPaymentUrl() = _rotationDetail.update { it.copy(paymentUrl = null) }

    private fun rotationDetailAction(id: String, block: suspend () -> Unit) {
        if (_rotationDetail.value.busy) return
        _rotationDetail.update { it.copy(busy = true, actionError = null) }
        viewModelScope.launch {
            try {
                block()
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                _rotationDetail.update { it.copy(actionError = e.toFailure()) }
            } finally {
                _rotationDetail.update { it.copy(busy = false) }
            }
            reloadRotation(id)
            loadMine(force = true)
        }
    }

    // ── Change delivery address ─────────────────────────────────────────────

    /** One change-address sheet's plumbing; the subscription and rotation screens each have one. */
    private inner class AddressChanger(
        val current: () -> List<DeliveryAddress>,
        val canChange: () -> Boolean,
        val get: () -> AddressChangeUiState,
        val set: ((AddressChangeUiState) -> AddressChangeUiState) -> Unit,
        /** The kitchens a new address must suit: one for a subscription, every remaining week's for a rotation. */
        val branchIds: () -> List<String>,
        val otherBusy: () -> Boolean,
        val save: suspend (List<SlotAddress>, String) -> List<DeliveryAddress>,
    ) {
        // One Idempotency-Key per body: a retried save is the same request.
        private var draft: List<SlotAddress>? = null
        private var key = ""

        fun open() {
            if (!canChange()) return
            val now = current()
            set {
                AddressChangeUiState(
                    visible = true,
                    loading = true,
                    allMeals = now.size < 2 || MealSlots.single(now) != null,
                    picks = now.associate { it.mealSlot to it.addressId },
                )
            }
            load()
        }

        fun load() {
            set { it.copy(loading = true, loadError = null) }
            viewModelScope.launch {
                try {
                    val items = api.addresses().items
                    set { it.copy(addresses = items, loading = false) }
                    checkCoverage(items)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Throwable) {
                    set { it.copy(loading = false, loadError = e.toFailure()) }
                }
            }
        }

        /** Marks the addresses one of the plan's kitchens does not deliver to. */
        private suspend fun checkCoverage(items: List<Address>) {
            val branches = branchIds()
            if (branches.isEmpty() || items.isEmpty()) return
            val cov = orNullUnlessCancelled { api.addressCoverage(branches, items.map { it.id }) } ?: return
            val outside = cov.items.filter { !it.delivers }
                .groupBy({ it.addressId }, { it.branchId })
                .mapValues { (_, b) -> b.minByOrNull { branches.indexOf(it) } ?: b.first() }
            set { it.copy(outsideBy = outside) }
        }

        /** Saves a new address from the sheet and picks it (for every meal when they share one). */
        fun add(label: String, building: String, area: String, city: String, at: LatLng) {
            val s = get()
            if (s.busy || otherBusy()) return
            set { it.copy(busy = true, addError = null) }
            viewModelScope.launch {
                try {
                    val a = api.createAddress(
                        AddressInput(
                            label = label.trim(),
                            building = building.trim().ifBlank { null },
                            area = area.trim().ifBlank { null },
                            city = city.trim(),
                            point = at,
                            isDefault = s.addresses.isEmpty(),
                        ),
                    )
                    set {
                        it.copy(
                            addresses = it.addresses + a,
                            picks = if (it.allMeals) current().associate { d -> d.mealSlot to a.id } else it.picks,
                            busy = false,
                            error = null,
                            addressAdded = it.addressAdded + 1,
                        )
                    }
                    // The checkout and rotation builder keep their own lists.
                    _checkout.update { it.copy(addresses = it.addresses + a) }
                    _rotation.update { it.copy(addresses = it.addresses + a) }
                    checkCoverage(get().addresses)
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Throwable) {
                    set { it.copy(busy = false, addError = e.toFailure()) }
                }
            }
        }

        fun allMeals(on: Boolean) = set { s ->
            // Back to one address: every meal follows the first meal's pick.
            val first = current().firstOrNull()?.mealSlot?.let { s.picks[it] }
            s.copy(allMeals = on, error = null, picks = if (on && first != null) s.picks.mapValues { first } else s.picks)
        }

        fun pick(slot: String, id: String) = set { it.copy(picks = it.picks + (slot to id), error = null) }

        fun pickAll(id: String) = set { s -> s.copy(picks = current().associate { it.mealSlot to id }, error = null) }

        fun save() {
            val s = get()
            if (s.busy || otherBusy()) return
            val moves = addressMoves(current(), s.picks)
            if (moves.isEmpty()) {
                set { AddressChangeUiState() }
                return
            }
            if (moves != draft) {
                draft = moves
                key = newIdempotencyKey()
            }
            val k = key
            set { it.copy(busy = true, error = null) }
            viewModelScope.launch {
                try {
                    val changed = save(moves, k)
                    draft = null
                    set { AddressChangeUiState(changed = changed) }
                    _mine.update { it.copy(loaded = false) }
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Throwable) {
                    set { it.copy(busy = false, error = AddressProblem.failureOf(e)) }
                }
            }
        }

        fun close() = set { if (it.busy) it else AddressChangeUiState(changed = it.changed) }

        fun dismissChanged() = set { it.copy(changed = emptyList()) }
    }

    private val subAddresses = AddressChanger(
        current = { _sub.value.subscription?.deliveryAddresses.orEmpty() },
        canChange = { _sub.value.subscription?.let { it.canChangeAddress && it.rotationId == null } == true },
        get = { _sub.value.addressChange },
        set = { f -> _sub.update { it.copy(addressChange = f(it.addressChange)) } },
        branchIds = { listOfNotNull(_sub.value.subscription?.branchId?.ifBlank { null }) },
        otherBusy = { _sub.value.busy },
        save = { moves, key ->
            val id = _sub.value.subscription?.id ?: error("no subscription")
            val res = api.changeSubscriptionAddresses(id, moves, key)
            _sub.update { it.copy(subscription = res.detail) }
            res.changed
        },
    )

    private val rotationAddresses = AddressChanger(
        current = { _rotationDetail.value.rotation?.deliveryAddresses.orEmpty() },
        canChange = { _rotationDetail.value.rotation?.canChangeAddress == true },
        get = { _rotationDetail.value.addressChange },
        set = { f -> _rotationDetail.update { it.copy(addressChange = f(it.addressChange)) } },
        // Every week still to come: the server checks them too and names the one that fails.
        branchIds = { _rotationDetail.value.rotation?.let(::remainingRotationBranches).orEmpty() },
        otherBusy = { _rotationDetail.value.busy },
        save = { moves, key ->
            val id = _rotationDetail.value.rotation?.id ?: error("no rotation")
            val res = api.changeRotationAddresses(id, moves, key)
            _rotationDetail.update { it.copy(rotation = res.detail) }
            res.changed
        },
    )

    /** Opens the change-address sheet for the open subscription (not for company lunches or rotation weeks). */
    fun openAddressChange() = subAddresses.open()
    fun retryAddressChangeLoad() = subAddresses.load()
    fun setAddressChangeAllMeals(on: Boolean) = subAddresses.allMeals(on)
    fun pickChangeAddress(slot: String, addressId: String) = subAddresses.pick(slot, addressId)
    fun pickChangeAddressForAll(addressId: String) = subAddresses.pickAll(addressId)
    /** Saves the meals whose pick changed; a retry after a failure reuses the same Idempotency-Key. */
    fun saveAddressChange() = subAddresses.save()
    fun closeAddressChange() = subAddresses.close()
    fun dismissAddressChanged() = subAddresses.dismissChanged()
    /** Saves a new address from the sheet and picks it. */
    fun addChangeAddress(label: String, building: String, area: String, city: String, at: LatLng) =
        subAddresses.add(label, building, area, city, at)

    /** The same for the open rotation: every remaining week moves, or none does. */
    fun openRotationAddressChange() = rotationAddresses.open()
    fun retryRotationAddressChangeLoad() = rotationAddresses.load()
    fun setRotationAddressChangeAllMeals(on: Boolean) = rotationAddresses.allMeals(on)
    fun pickRotationChangeAddress(slot: String, addressId: String) = rotationAddresses.pick(slot, addressId)
    fun pickRotationChangeAddressForAll(addressId: String) = rotationAddresses.pickAll(addressId)
    fun saveRotationAddressChange() = rotationAddresses.save()
    fun closeRotationAddressChange() = rotationAddresses.close()
    fun dismissRotationAddressChanged() = rotationAddresses.dismissChanged()
    fun addRotationChangeAddress(label: String, building: String, area: String, city: String, at: LatLng) =
        rotationAddresses.add(label, building, area, city, at)

    /** The kitchens of the weeks that have not ended (in week order), whose areas a new address must suit. */
    private fun remainingRotationBranches(r: Rotation): List<String> {
        val today = Clock.System.now().toLocalDateTime(DUBAI).date.toString()
        return r.weeks.sortedBy { it.week }
            .filter { it.endsOn >= today && it.status != "cancelled" && it.branchId.isNotBlank() }
            .map { it.branchId }
            .distinct()
    }

    /** Drops the account's own data when the session ends. */
    fun forget() {
        _mine.value = MyMealsUiState()
        _sub.value = SubscriptionUiState()
        _renew.value = RenewUiState()
        _kitchen.update { it.copy(taste = null, tasteError = null) }
        _rotation.update { it.copy(adventure = null) }
        _rotationDetail.value = RotationDetailUiState()
        _checkout.update { it.copy(addresses = emptyList(), addressId = null) }
    }

    private fun earliestStart(cutoffHours: Int): LocalDate =
        earliestStartAt(cutoffHours, Clock.System.now().toLocalDateTime(DUBAI))

    /** The first allowed start a `too_soon` refusal names (YYYY-MM-DD), or null. */
    private fun earliestFromTooSoon(f: Failure): String? {
        val r = f as? Failure.Rejected ?: return null
        if (r.fieldCodes["start_date"] != "too_soon") return null
        return Regex("""\d{4}-\d{2}-\d{2}""").find(r.fieldMessages["start_date"].orEmpty())?.value
    }

    companion object {
        /**
         * The first day still open at [now]: a day closes at its midnight minus
         * the cutoff. Same rule as the API's subscriptions.EarliestStart and the
         * web's earliestStart.
         */
        internal fun earliestStartAt(cutoffHours: Int, now: kotlinx.datetime.LocalDateTime): LocalDate {
            val minutes = now.hour * 60 + now.minute
            return now.date.plus(DatePeriod(days = (cutoffHours * 60 + minutes) / 1440 + 1))
        }

        const val ROTATION_KITCHENS = 4
        private val DUBAI = TimeZone.of("Asia/Dubai")
    }
}
