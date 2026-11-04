package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.CollectionKitchens
import ae.marketplace.mobile.model.DayDish
import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.model.AddressInput
import ae.marketplace.mobile.model.HomeSection
import ae.marketplace.mobile.model.Kitchen
import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.KitchenReview
import ae.marketplace.mobile.model.LatLng
import ae.marketplace.mobile.model.MealPlan
import ae.marketplace.mobile.model.MyReview
import ae.marketplace.mobile.model.MySubscription
import ae.marketplace.mobile.model.PromoOffer
import ae.marketplace.mobile.model.Rotation
import ae.marketplace.mobile.model.RotationInput
import ae.marketplace.mobile.model.ScheduleRule
import ae.marketplace.mobile.model.SubscribeInput
import ae.marketplace.mobile.model.SubscriptionPreview
import ae.marketplace.mobile.network.Failure
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
)

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
) {
    val ready: Boolean
        get() = plan != null && addressId != null && windowId != null &&
            weekdays.size == plan.daysPerWeek && slots.size == plan.mealsPerDay &&
            validStart(startDate, earliestStart)
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
    /** Sent with the create, one per draft, so a create retried after a timeout is not a second rotation. */
    val idempotencyKey: String = newIdempotencyKey(),
    val candidatesLoaded: Boolean = false,
    /** The rotation is created and its payment went through (or was handed to the provider). */
    val paid: Boolean = false,
) {
    /** Fewer kitchens deliver here than a rotation needs. */
    val tooFewKitchens: Boolean
        get() = candidatesLoaded && candidates.size < MealsViewModel.ROTATION_KITCHENS
}

@OptIn(ExperimentalUuidApi::class)
private fun newIdempotencyKey(): String = Uuid.random().toString()

data class MyMealsUiState(
    val subscriptions: List<MySubscription> = emptyList(),
    val rotations: List<Rotation> = emptyList(),
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
)

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

    private val _mine = MutableStateFlow(MyMealsUiState())
    val mine: StateFlow<MyMealsUiState> = _mine.asStateFlow()

    private val _sub = MutableStateFlow(SubscriptionUiState())
    val sub: StateFlow<SubscriptionUiState> = _sub.asStateFlow()

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
                            runCatching {
                                api.home(at).sections.orEmpty().filter { it.enabled && it.type != "all_kitchens" }
                            }.getOrNull()
                        }
                    } else {
                        null
                    }
                    val dishes: Deferred<List<DayDish>>? = if (unfiltered) {
                        async { runCatching { api.tomorrowDishes(at).items }.getOrDefault(emptyList()) }
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
            loadReviews(branchId)
        }
    }

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
                refreshPreview()
            } catch (e: Throwable) {
                _checkout.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
    }

    fun selectAddress(id: String) { _checkout.update { it.copy(addressId = id) }; refreshPreview() }

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
        return SubscribeInput(
            planId = s.plan!!.id,
            addressId = s.addressId!!,
            startDate = s.startDate,
            schedule = s.weekdays.sorted().flatMap { d -> s.slots.map { ScheduleRule(d, it, window) } },
            promoCode = s.promoCode.ifBlank { null },
        )
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
                _checkout.update { it.copy(preview = p, error = null) }
            } catch (e: Throwable) {
                _checkout.update { it.copy(preview = null, error = e.toFailure()) }
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
                _checkout.update { it.copy(busy = false, error = e.toFailure()) }
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
            } catch (e: Throwable) {
                _rotation.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
        loadAddresses()
    }

    fun togglePick(branchId: String) {
        _rotation.update { s ->
            val next = if (branchId in s.picked) s.picked - branchId
            else if (s.picked.size < ROTATION_KITCHENS) s.picked + branchId else s.picked
            s.copy(picked = next, quote = null).newDraft()
        }
        quoteRotation()
    }

    fun selectRotationAddress(id: String) { _rotation.update { it.copy(addressId = id, quote = null).newDraft() }; quoteRotation() }

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
        return RotationInput(
            planIds = plans.map { it.id },
            addressId = address,
            startDate = s.startDate,
            deliveryWeekdays = days.sorted().take(perWeek),
            mealSlots = slots.sorted().take(perDay),
        )
    }

    private fun quoteRotation() {
        val input = rotationInput(_rotation.value) ?: return
        viewModelScope.launch {
            try {
                val q = api.previewRotation(input)
                _rotation.update { it.copy(quote = q, error = null) }
            } catch (e: Throwable) {
                _rotation.update { it.copy(error = e.toFailure()) }
            }
        }
    }

    fun payRotation() {
        val draft = _rotation.value
        if (draft.busy) return
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
        _mine.update {
            it.copy(
                companies = companies,
                // Rotation weeks show under their rotation, not as four more rows.
                subscriptions = subs.filter { s -> s.rotationId == null },
                rotations = rotations,
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
        val next = api.renew(id)
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
        val next = api.renewRotation(id)
        api.payRotation(next.id).redirectUrl?.let { url -> _mine.update { it.copy(paymentUrl = url) } }
    }

    fun dismissRotationError() = _mine.update { it.copy(actionError = null) }

    /** Drops the account's own data when the session ends. */
    fun forget() {
        _mine.value = MyMealsUiState()
        _sub.value = SubscriptionUiState()
        _checkout.update { it.copy(addresses = emptyList(), addressId = null) }
    }

    private fun earliestStart(cutoffHours: Int): LocalDate =
        earliestStartAt(cutoffHours, Clock.System.now().toLocalDateTime(DUBAI))

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
