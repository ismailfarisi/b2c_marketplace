package ae.marketplace.mobile.meals

import ae.marketplace.mobile.model.DayDish
import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.model.AddressInput
import ae.marketplace.mobile.model.HomeSection
import ae.marketplace.mobile.model.Kitchen
import ae.marketplace.mobile.model.KitchenDetail
import ae.marketplace.mobile.model.KitchenReview
import ae.marketplace.mobile.model.LatLng
import ae.marketplace.mobile.model.MealPlan
import ae.marketplace.mobile.model.MySubscription
import ae.marketplace.mobile.model.Rotation
import ae.marketplace.mobile.model.RotationInput
import ae.marketplace.mobile.model.ScheduleRule
import ae.marketplace.mobile.model.SubscribeInput
import ae.marketplace.mobile.model.SubscriptionPreview
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.toFailure
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
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

data class StorefrontUiState(
    val at: LatLng? = null,
    val slot: String? = null,
    val sections: List<HomeSection> = emptyList(),
    val kitchens: List<Kitchen> = emptyList(),
    /** The next delivery day's dishes; empty when that call fails, which never blocks the page. */
    val dishes: List<DayDish> = emptyList(),
    val loading: Boolean = false,
    val error: Failure? = null,
)

data class KitchenUiState(
    val branchId: String = "",
    val kitchen: KitchenDetail? = null,
    val reviews: List<KitchenReview> = emptyList(),
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
    /** Set once the order exists and needs paying on the provider's page. */
    val paymentUrl: String? = null,
    /** Set once paid (or once paying is handed to the browser); the screen moves on. */
    val subscriptionId: String? = null,
) {
    val ready: Boolean
        get() = plan != null && addressId != null && windowId != null &&
            weekdays.size == plan.daysPerWeek && slots.size == plan.mealsPerDay
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
    val rotationId: String? = null,
)

data class MyMealsUiState(
    val subscriptions: List<MySubscription> = emptyList(),
    val rotations: List<Rotation> = emptyList(),
    val loading: Boolean = false,
    val loaded: Boolean = false,
    val error: Failure? = null,
)

data class SubscriptionUiState(
    val subscription: MySubscription? = null,
    val loading: Boolean = false,
    val busy: Boolean = false,
    val error: Failure? = null,
    val paymentUrl: String? = null,
)

/**
 * The meals tab: storefront, kitchen, checkout, rotation and the customer's
 * subscriptions. One holder for the tab, like MeViewModel, so going from a
 * kitchen to checkout and back keeps what was loaded.
 */
@OptIn(ExperimentalTime::class)
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

    private var storeJob: Job? = null
    private var previewJob: Job? = null

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

    fun loadStore() {
        val at = _store.value.at ?: return
        val slot = _store.value.slot
        storeJob?.cancel()
        storeJob = viewModelScope.launch {
            _store.update { it.copy(loading = true, error = null) }
            try {
                val sections = if (slot == null) {
                    api.home(at).sections.orEmpty().filter { it.enabled && it.type != "all_kitchens" }
                } else {
                    emptyList()
                }
                val kitchens = api.kitchens(at, slot).items
                val dishes = if (slot == null) {
                    runCatching { api.tomorrowDishes(at).items }.getOrDefault(emptyList())
                } else {
                    emptyList()
                }
                _store.update { it.copy(sections = sections, kitchens = kitchens, dishes = dishes, loading = false) }
            } catch (e: Throwable) {
                _store.update { it.copy(loading = false, error = e.toFailure()) }
            }
        }
    }

    // ── Kitchen ─────────────────────────────────────────────────────────────

    fun openKitchen(branchId: String) {
        val at = _store.value.at ?: return
        _kitchen.value = KitchenUiState(branchId = branchId, loading = true)
        viewModelScope.launch {
            try {
                val k = api.kitchen(branchId, at)
                _kitchen.update { it.copy(kitchen = k, loading = false) }
                val reviews = runCatching { api.reviews(branchId).items }.getOrDefault(emptyList())
                _kitchen.update { it.copy(reviews = reviews) }
            } catch (e: Throwable) {
                _kitchen.update { it.copy(loading = false, error = e.toFailure()) }
            }
        }
    }

    // ── Checkout ────────────────────────────────────────────────────────────

    fun startCheckout(plan: MealPlan) {
        val k = _kitchen.value.kitchen ?: return
        _checkout.value = CheckoutUiState(
            kitchen = k,
            plan = plan,
            weekdays = plan.deliveryWeekdays.take(plan.daysPerWeek).toSet(),
            slots = plan.mealSlots.take(plan.mealsPerDay).toSet(),
            windowId = k.deliveryWindows.firstOrNull()?.id,
            startDate = earliestStart(plan.cutoffHours).toString(),
        )
        loadAddresses()
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

    fun addAddress(label: String, building: String, area: String, city: String, at: LatLng) {
        viewModelScope.launch {
            _checkout.update { it.copy(busy = true, error = null) }
            try {
                val a = api.createAddress(
                    AddressInput(
                        label = label.ifBlank { "Home" },
                        building = building.ifBlank { null },
                        area = area.ifBlank { null },
                        city = city.ifBlank { "Dubai" },
                        point = at,
                        isDefault = _checkout.value.addresses.isEmpty(),
                    ),
                )
                _checkout.update { it.copy(addresses = it.addresses + a, addressId = a.id, busy = false) }
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

    /** Creates the subscription and pays its first order. */
    fun subscribe() {
        val input = subscribeInput(_checkout.value) ?: return
        viewModelScope.launch {
            _checkout.update { it.copy(busy = true, error = null) }
            try {
                val created = api.subscribe(input)
                val intent = api.payOrder(created.orderId)
                _checkout.update {
                    it.copy(busy = false, paymentUrl = intent.redirectUrl, subscriptionId = created.id)
                }
                _mine.update { it.copy(loaded = false) }
            } catch (e: Throwable) {
                _checkout.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
    }

    fun paymentOpened() {
        _checkout.update { it.copy(paymentUrl = null) }
        _rotation.update { it.copy(paymentUrl = null) }
        _sub.update { it.copy(paymentUrl = null) }
    }

    // ── Rotation ────────────────────────────────────────────────────────────

    fun startRotation() {
        val at = _store.value.at ?: return
        _rotation.value = RotationUiState(busy = true, startDate = earliestStart(48).toString())
        viewModelScope.launch {
            try {
                val kitchens = api.kitchens(at, rotation = true).items
                val plans = kitchens.associate { k ->
                    k.branchId to api.kitchen(k.branchId, at).plans.firstOrNull { it.rotationEnabled }
                }.filterValues { it != null }.mapValues { it.value!! }
                _rotation.update { it.copy(candidates = kitchens.filter { k -> k.branchId in plans }, plans = plans, busy = false) }
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
            s.copy(picked = next, quote = null)
        }
        quoteRotation()
    }

    fun selectRotationAddress(id: String) { _rotation.update { it.copy(addressId = id, quote = null) }; quoteRotation() }

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
        val input = rotationInput(_rotation.value) ?: return
        viewModelScope.launch {
            _rotation.update { it.copy(busy = true, error = null) }
            try {
                val r = api.createRotation(input)
                val intent = api.payRotation(r.id)
                _rotation.update { it.copy(busy = false, paymentUrl = intent.redirectUrl, rotationId = r.id) }
                _mine.update { it.copy(loaded = false) }
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
                val subs = api.subscriptions().items
                val rotations = api.rotations().items
                _mine.update {
                    it.copy(
                        // Rotation weeks show under their rotation, not as four more rows.
                        subscriptions = subs.filter { s -> s.rotationId == null },
                        rotations = rotations,
                        loading = false,
                        loaded = true,
                    )
                }
            } catch (e: Throwable) {
                _mine.update { it.copy(loading = false, error = e.toFailure()) }
            }
        }
    }

    fun openSubscription(id: String) {
        _sub.value = SubscriptionUiState(loading = true)
        reloadSubscription(id)
    }

    private fun reloadSubscription(id: String) {
        viewModelScope.launch {
            try {
                val s = api.subscription(id)
                _sub.update { it.copy(subscription = s, loading = false, busy = false) }
            } catch (e: Throwable) {
                _sub.update { it.copy(loading = false, busy = false, error = e.toFailure()) }
            }
        }
    }

    private fun act(block: suspend (String) -> Unit) {
        val id = _sub.value.subscription?.id ?: return
        viewModelScope.launch {
            _sub.update { it.copy(busy = true, error = null) }
            try {
                block(id)
                _mine.update { it.copy(loaded = false) }
                reloadSubscription(id)
            } catch (e: Throwable) {
                _sub.update { it.copy(busy = false, error = e.toFailure()) }
            }
        }
    }

    fun choose(date: String, slot: String, dishId: String) = act { api.choose(it, date, slot, dishId) }
    fun skip(date: String, slot: String) = act { api.skip(it, date, slot) }
    fun pause(from: String, to: String) = act { api.pause(it, from, to) }
    fun cancel() = act { api.cancel(it, "customer_request") }
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

    fun cancelRotation(id: String) {
        viewModelScope.launch {
            try { api.cancelRotation(id, "customer_request"); loadMine(force = true) }
            catch (e: Throwable) { _mine.update { it.copy(error = e.toFailure()) } }
        }
    }

    fun setRotationRenewal(id: String, auto: Boolean) {
        viewModelScope.launch {
            try { api.setRotationRenewal(id, auto); loadMine(force = true) }
            catch (e: Throwable) { _mine.update { it.copy(error = e.toFailure()) } }
        }
    }

    /** Drops the account's own data when the session ends. */
    fun forget() {
        _mine.value = MyMealsUiState()
        _sub.value = SubscriptionUiState()
        _checkout.update { it.copy(addresses = emptyList(), addressId = null) }
    }

    private fun earliestStart(cutoffHours: Int): LocalDate {
        val today = Clock.System.now().toLocalDateTime(DUBAI).date
        return today.plus(DatePeriod(days = 1 + (cutoffHours + 23) / 24))
    }

    companion object {
        const val ROTATION_KITCHENS = 4
        private val DUBAI = TimeZone.of("Asia/Dubai")
    }
}
