package ae.marketplace.mobile.meals

import ae.marketplace.mobile.identity.ViewerIdentity
import ae.marketplace.mobile.model.DeliveryAddress
import ae.marketplace.mobile.model.MealSlots
import ae.marketplace.mobile.model.ScheduleRule
import ae.marketplace.mobile.model.SlotAddress
import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.InMemoryTokenStore
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.MockRequestHandleScope
import io.ktor.client.engine.mock.respond
import io.ktor.client.request.HttpRequestData
import io.ktor.client.request.HttpResponseData
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.content.OutgoingContent
import io.ktor.http.headersOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class MealAddressesTest {
    private val json = headersOf(HttpHeaders.ContentType, "application/json")

    @BeforeTest fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())
    @AfterTest fun tearDown() = Dispatchers.resetMain()

    private class Seen(val method: HttpMethod, val path: String, val body: JsonObject?, val key: String?)

    private val seen = mutableListOf<Seen>()

    private fun vm(handler: suspend MockRequestHandleScope.(HttpRequestData) -> HttpResponseData) =
        MealsViewModel(
            MealsApi(
                ApiClientFactory.create(
                    baseUrl = "https://api.test/v1",
                    tokens = InMemoryTokenStore(),
                    identity = ViewerIdentity(deviceId = "test-device"),
                    engine = MockEngine { req ->
                        val text = (req.body as? OutgoingContent.ByteArrayContent)?.bytes()?.decodeToString()
                        lock.withLock {
                            seen += Seen(req.method, req.url.encodedPath.substringAfter("/v1/"), text?.let { Json.parseToJsonElement(it).jsonObject }, req.headers["Idempotency-Key"])
                        }
                        handler(req)
                    },
                ),
            ),
        )

    private val lock = Mutex()

    private suspend fun requests(path: String) = lock.withLock { seen.filter { it.path == path } }

    /** MockEngine answers off the test scheduler, so wait on real time. */
    private suspend fun <T> StateFlow<T>.until(check: (T) -> Boolean): T = withContext(Dispatchers.Default) {
        withTimeout(5_000) { first(check) }
    }

    private suspend fun waitFor(what: suspend () -> Boolean) = withContext(Dispatchers.Default) {
        withTimeout(5_000) { while (!what()) kotlinx.coroutines.delay(10) }
    }

    // ── pure helpers ──────────────────────────────────────────────────────

    @Test
    fun outsideMessageNamesKitchenMealAndWeek() {
        assertEquals(
            AddressProblem("outside", kitchen = "Teta's Kitchen", slot = "dinner", week = 3),
            AddressProblem.parseOutside("Teta's Kitchen does not deliver dinner to that address (week 3)"),
        )
        assertEquals(
            AddressProblem("outside", kitchen = null, slot = "lunch"),
            AddressProblem.parseOutside("this kitchen does not deliver lunch to that address"),
        )
        assertEquals(AddressProblem("outside", kitchen = "Spice Route"), AddressProblem.parseOutside("Spice Route does not deliver to that address"))
        assertEquals(AddressProblem("outside"), AddressProblem.parseOutside("something else"))
    }

    @Test
    fun checkoutRefusalsLandOnTheirMeal() {
        val schedule = listOf(
            ScheduleRule(0, "lunch", "w", "office"),
            ScheduleRule(0, "dinner", "w", "home"),
            ScheduleRule(1, "lunch", "w", "office"),
        )
        val f = Failure.Rejected(
            fieldCodes = mapOf("schedule[1].address_id" to "outside_delivery_area"),
            fieldMessages = mapOf("schedule[1].address_id" to "this kitchen does not deliver dinner to that address"),
        )
        val by = AddressProblem.bySlot(f, schedule, defaultAddress = "office", perMeal = true)
        assertEquals(setOf("dinner"), by.keys)
        assertEquals("outside", by.getValue("dinner").kind)

        val default = Failure.Rejected(fieldCodes = mapOf("address_id" to "not_found"))
        assertEquals(setOf("lunch"), AddressProblem.bySlot(default, schedule, "office", perMeal = true).keys)
        assertTrue(AddressProblem.bySlot(default, schedule, "office", perMeal = false).isEmpty())
    }

    @Test
    fun changeProblemsCoverConflictsAndMissingAddresses() {
        assertEquals("company", AddressProblem.ofChange(Failure.Rejected(code = "company_lunch"))?.kind)
        assertEquals("gone", AddressProblem.ofChange(Failure.Rejected(code = "address_not_found"))?.kind)
        val outside = AddressProblem.ofChange(
            Failure.Rejected(
                fieldCodes = mapOf("addresses" to "outside_delivery_area"),
                fieldMessages = mapOf("addresses" to "Green Bowl does not deliver lunch to that address (week 2)"),
            ),
        )
        assertEquals(AddressProblem("outside", "Green Bowl", "lunch", 2), outside)
        assertNull(AddressProblem.ofChange(Failure.Offline))
        // One field per refused meal: the first is shown, every one is known.
        val two = Failure.Rejected(
            code = "validation_failed",
            fieldCodes = mapOf("addresses[dinner]" to "outside_delivery_area", "addresses[lunch]" to "outside_delivery_area"),
            fieldMessages = mapOf(
                "addresses[dinner]" to "Green Bowl does not deliver dinner to that address",
                "addresses[lunch]" to "Green Bowl does not deliver lunch to that address",
            ),
        )
        assertEquals("dinner", AddressProblem.ofChange(two)?.slot)
        assertEquals(listOf("dinner", "lunch"), AddressProblem.ofChangeAll(two).map { it.slot })
    }

    @Test
    fun onlyMovedMealsAreSent() {
        val current = listOf(DeliveryAddress("lunch", "home"), DeliveryAddress("dinner", "home"))
        assertEquals(listOf(SlotAddress("lunch", "office")), addressMoves(current, mapOf("lunch" to "office", "dinner" to "home")))
        assertTrue(addressMoves(current, mapOf("lunch" to "home")).isEmpty())
        assertEquals(listOf("breakfast", "lunch", "dinner"), MealSlots.sorted(listOf("dinner", "breakfast", "lunch")))
        assertEquals("home", MealSlots.single(current)?.addressId)
        assertNull(MealSlots.single(listOf(DeliveryAddress("lunch", "office"), DeliveryAddress("dinner", "home"))))
    }

    // ── checkout ──────────────────────────────────────────────────────────

    private val addresses = """{"items":[
        {"id":"home","label":"Home","city":"Dubai","point":{"lat":25.1,"lng":55.2},"is_default":true},
        {"id":"office","label":"Office","city":"Dubai","point":{"lat":25.2,"lng":55.3}},
        {"id":"far","label":"Al Ain","city":"Al Ain","point":{"lat":24.2,"lng":55.7}}]}"""

    private val kitchen = """{"branch_id":"b1","name":"Teta","delivery_windows":[{"id":"w1","label":"12-2"}],
        "plan_list":[{"id":"p1","name":"Two a day","cycle_days":28,"days_per_week":5,"meals_per_day":2,
        "meal_slots":["dinner","lunch"],"delivery_weekdays":[0,1,2,3,4],"cutoff_hours":24,"price_minor":100000}]}"""

    private val coverage = """{"items":[
        {"branch_id":"b1","address_id":"home","delivers":true},
        {"branch_id":"b1","address_id":"office","delivers":true},
        {"branch_id":"b1","address_id":"far","delivers":false}]}"""

    private val preview = """{"subtotal_minor":100000,"total_minor":100000}"""

    private fun checkoutVm(previewAnswer: (HttpRequestData) -> Pair<HttpStatusCode, String> = { HttpStatusCode.OK to preview }) = vm { req ->
        val path = req.url.encodedPath
        when {
            path.endsWith("meals/kitchens/b1") -> respond(kitchen, HttpStatusCode.OK, json)
            path.endsWith("me/addresses/coverage") -> respond(coverage, HttpStatusCode.OK, json)
            path.endsWith("me/addresses") -> respond(addresses, HttpStatusCode.OK, json)
            path.endsWith("subscriptions/preview") -> previewAnswer(req).let { (st, body) -> respond(body, st, json) }
            else -> respond("""{"items":[]}""", HttpStatusCode.OK, json)
        }
    }

    private suspend fun MealsViewModel.atCheckout() {
        setLocation(25.0, 55.0)
        openKitchen("b1")
        val k = kitchen.until { it.kitchen != null }.kitchen!!
        startCheckout(k.plans.first())
        checkout.until { it.addresses.isNotEmpty() && it.outsideAddresses.isNotEmpty() }
    }

    @Test
    fun aRemovedHomeAddressFallsBackToTheDefaultAtCheckout() = runTest {
        val vm = checkoutVm()
        vm.setLocation(25.0, 55.0)
        vm.openKitchen("b1")
        val k = vm.kitchen.until { it.kitchen != null }.kitchen!!
        vm.startCheckout(k.plans.first())
        vm.selectAddress("gone") // the browse place is a saved address since removed
        val s = vm.checkout.until { it.addresses.isNotEmpty() }
        assertEquals("home", s.addressId)
        vm.selectAddress("gone")
        assertEquals("home", vm.checkout.value.addressId)
    }

    @Test
    fun perMealCheckoutSendsEachMealsAddressAndBlocksAnUncoveredOne() = runTest {
        val vm = checkoutVm()
        vm.atCheckout()
        assertTrue(vm.checkout.value.canSplitAddresses)
        assertTrue(vm.checkout.value.outside("far"))

        vm.setPerMealAddresses(true)
        vm.selectSlotAddress("dinner", "far")
        val blocked = vm.checkout.value
        assertEquals(listOf("dinner"), blocked.uncoveredSlots)
        assertFalse(blocked.ready, "a meal headed outside the kitchen's area blocks checkout")

        vm.selectSlotAddress("lunch", "office")
        vm.selectSlotAddress("dinner", "home")
        assertTrue(vm.checkout.value.ready)
        waitFor { requests("subscriptions/preview").any { r -> r.body?.get("address_id")?.jsonPrimitive?.content == "office" && r.body["schedule"]!!.jsonArray.any { it.jsonObject["address_id"]?.jsonPrimitive?.content == "home" } } }
        val body = requests("subscriptions/preview").last().body!!
        assertEquals("office", body["address_id"]!!.jsonPrimitive.content, "the first meal of the day's address is the default")
        val rules = body["schedule"]!!.jsonArray.map { it.jsonObject }
        assertEquals(10, rules.size)
        assertTrue(rules.filter { it["meal_slot"]!!.jsonPrimitive.content == "lunch" }.all { it["address_id"]!!.jsonPrimitive.content == "office" })
        assertTrue(rules.filter { it["meal_slot"]!!.jsonPrimitive.content == "dinner" }.all { it["address_id"]!!.jsonPrimitive.content == "home" })
    }

    @Test
    fun oneAddressCheckoutSendsNoPerMealAddresses() = runTest {
        val vm = checkoutVm()
        vm.atCheckout()
        waitFor { requests("subscriptions/preview").isNotEmpty() }
        val body = requests("subscriptions/preview").last().body!!
        assertEquals("home", body["address_id"]!!.jsonPrimitive.content)
        assertTrue(body["schedule"]!!.jsonArray.none { "address_id" in it.jsonObject })
        vm.selectAddress("far")
        assertTrue(vm.checkout.value.addressOutside)
        assertFalse(vm.checkout.value.ready)
    }

    @Test
    fun serverRefusalOfOneMealsAddressShowsOnThatMeal() = runTest {
        // Only dinner-to-Office is refused, so earlier previews in flight
        // can't mark another address outside and stop the preview.
        val vm = checkoutVm { req ->
            val body = (req.body as? OutgoingContent.ByteArrayContent)?.bytes()?.decodeToString().orEmpty()
            if (Regex(""""meal_slot":"dinner"[^}]*"address_id":"office"""").containsMatchIn(body)) {
                HttpStatusCode.UnprocessableEntity to """{"title":"invalid","status":422,"errors":[{"field":"schedule[1].address_id","code":"outside_delivery_area","message":"this kitchen does not deliver dinner to that address"}]}"""
            } else {
                HttpStatusCode.OK to preview
            }
        }
        vm.atCheckout()
        vm.setPerMealAddresses(true)
        vm.selectSlotAddress("dinner", "office")
        val s = vm.checkout.until { it.slotProblems.isNotEmpty() }
        assertEquals(setOf("dinner"), s.slotProblems.keys)
        assertEquals("dinner", s.slotProblems.getValue("dinner").slot)
    }

    // ── rotation builder ──────────────────────────────────────────────────

    @Test
    fun rotationKitchenNotDeliveringToADinnerAddressCannotBePicked() = runTest {
        val kitchens = (1..5).joinToString(",", "{\"items\":[", "]}") { """{"branch_id":"k$it","name":"K$it","rotation_week_price_minor":30000}""" }
        fun detail(id: String) = """{"branch_id":"$id","name":"$id","plan_list":[{"id":"p-$id","name":"Rot","cycle_days":7,"days_per_week":5,
            "meals_per_day":2,"meal_slots":["lunch","dinner"],"delivery_weekdays":[0,1,2,3,4],"cutoff_hours":48,"price_minor":30000,"rotation_enabled":true}]}"""
        val vm = vm { req ->
            val path = req.url.encodedPath
            when {
                path.endsWith("meals/kitchens") -> respond(kitchens, HttpStatusCode.OK, json)
                Regex("meals/kitchens/(k\\d)$").find(path) != null -> respond(detail(path.substringAfterLast('/')), HttpStatusCode.OK, json)
                path.endsWith("me/addresses/coverage") -> {
                    val body = Json.parseToJsonElement((req.body as OutgoingContent.ByteArrayContent).bytes().decodeToString()).jsonObject
                    val ids = body["address_ids"]!!.jsonArray.map { it.jsonPrimitive.content }
                    val items = (1..5).flatMap { k -> ids.map { a -> """{"branch_id":"k$k","address_id":"$a","delivers":${!(k == 2 && a == "office")}}""" } }
                    respond(items.joinToString(",", "{\"items\":[", "]}"), HttpStatusCode.OK, json)
                }
                path.endsWith("me/addresses") -> respond(addresses, HttpStatusCode.OK, json)
                path.endsWith("rotations/preview") -> respond("""{"start_date":"2026-12-01","end_date":"2026-12-28","weeks":[],"amount_minor":120000}""", HttpStatusCode.OK, json)
                else -> respond("""{}""", HttpStatusCode.OK, json)
            }
        }
        vm.setLocation(25.0, 55.0)
        vm.startRotation()
        vm.rotation.until { it.candidatesLoaded && it.addressId != null && it.outside["home"] != null }
        assertFalse(vm.rotation.value.unavailable("k2"), "k2 delivers to home")

        vm.setRotationPerMeal(true)
        vm.selectRotationSlotAddress("lunch", "office")
        val s = vm.rotation.until { it.outside["office"] != null }
        assertEquals("Office", s.unavailableAt("k2")?.label)
        vm.togglePick("k2")
        assertFalse("k2" in vm.rotation.value.picked, "a kitchen that does not deliver lunch to the office is not pickable")

        listOf("k1", "k3", "k4", "k5").forEach(vm::togglePick)
        waitFor { requests("rotations/preview").isNotEmpty() }
        val body = requests("rotations/preview").last().body!!
        assertEquals("office", body["address_id"]!!.jsonPrimitive.content)
        val perSlot = body["slot_addresses"]!!.jsonArray.map { it.jsonObject["meal_slot"]!!.jsonPrimitive.content to it.jsonObject["address_id"]!!.jsonPrimitive.content }
        assertEquals(listOf("dinner" to "home"), perSlot)
    }

    // ── change address ────────────────────────────────────────────────────

    private fun detail(lunch: String, effective: String? = null) = """{"id":"s1","status":"active","branch_id":"b1","kitchen_name":"Teta",
        "can_change_address":true,"delivery_addresses":[
        {"meal_slot":"lunch","address_id":"$lunch","label":"${if (lunch == "office") "Office" else "Home"}","line":"","effective_from":${effective?.let { "\"$it\"" } ?: "null"}},
        {"meal_slot":"dinner","address_id":"home","label":"Home","line":"","effective_from":null}]"""

    @Test
    fun changingTheLunchAddressSendsOnlyLunchAndShowsWhenItStarts() = runTest {
        var failNext = true
        val firstSaveHeld = kotlinx.coroutines.CompletableDeferred<Unit>()
        val vm = vm { req ->
            val path = req.url.encodedPath
            when {
                path.endsWith("me/subscriptions/s1") -> respond(detail("home") + "}", HttpStatusCode.OK, json)
                path.endsWith("me/addresses/coverage") -> respond(coverage, HttpStatusCode.OK, json)
                path.endsWith("me/addresses") -> respond(addresses, HttpStatusCode.OK, json)
                path.endsWith("subscriptions/s1/addresses") && failNext -> {
                    failNext = false
                    firstSaveHeld.await()
                    respond("""{"title":"down"}""", HttpStatusCode.ServiceUnavailable, json)
                }
                path.endsWith("subscriptions/s1/addresses") -> respond(
                    detail("office", "2026-11-12") + ""","changed":[{"meal_slot":"lunch","address_id":"office","label":"Office","line":"","effective_from":"2026-11-12"}]}""",
                    HttpStatusCode.OK, json,
                )
                else -> respond("""{"items":[]}""", HttpStatusCode.OK, json)
            }
        }
        vm.openSubscription("s1")
        vm.sub.until { it.subscription != null }
        vm.openAddressChange()
        val open = vm.sub.until { it.addressChange.visible && it.addressChange.addresses.isNotEmpty() && it.addressChange.outsideAddresses.isNotEmpty() }
        assertTrue(open.addressChange.allMeals, "both meals go home: one picker")
        assertTrue(open.addressChange.outside("far"))

        vm.setAddressChangeAllMeals(false)
        vm.pickChangeAddress("lunch", "office")
        vm.saveAddressChange()
        vm.saveAddressChange() // double tap while saving is ignored
        firstSaveHeld.complete(Unit)
        val failed = vm.sub.until { it.addressChange.error != null }
        assertTrue(failed.addressChange.visible, "a failed save keeps the sheet and the picks")
        vm.saveAddressChange()
        val done = vm.sub.until { it.addressChange.changed.isNotEmpty() }

        val puts = requests("subscriptions/s1/addresses")
        assertEquals(2, puts.size)
        assertEquals(puts[0].key, puts[1].key, "a retried save reuses its Idempotency-Key")
        val sent = puts.last().body!!["addresses"]!!.jsonArray.map { it.jsonObject["meal_slot"]!!.jsonPrimitive.content to it.jsonObject["address_id"]!!.jsonPrimitive.content }
        assertEquals(listOf("lunch" to "office"), sent)
        assertFalse(done.addressChange.visible)
        assertEquals("2026-11-12", done.addressChange.changed.single().effectiveFrom)
        assertEquals("office", done.subscription!!.deliveryAddresses.first { it.mealSlot == "lunch" }.addressId)
    }

    @Test
    fun rotationChangeRefusedForOneWeekNamesKitchenAndWeek() = runTest {
        val rotation = """{"id":"r1","status":"active","start_date":"2026-11-01","end_date":"2026-11-28","weeks":[],"amount_minor":1,
            "can_change_address":true,"delivery_addresses":[{"meal_slot":"lunch","address_id":"home","label":"Home","line":"","effective_from":null}]}"""
        val vm = vm { req ->
            val path = req.url.encodedPath
            when {
                path.endsWith("me/rotations/r1") -> respond(rotation, HttpStatusCode.OK, json)
                path.endsWith("me/addresses") -> respond(addresses, HttpStatusCode.OK, json)
                path.endsWith("rotations/r1/addresses") -> respond(
                    """{"title":"invalid","status":422,"errors":[{"field":"addresses","code":"outside_delivery_area","message":"Green Bowl does not deliver lunch to that address (week 3)"}]}""",
                    HttpStatusCode.UnprocessableEntity, json,
                )
                else -> respond("""{"items":[]}""", HttpStatusCode.OK, json)
            }
        }
        vm.openRotation("r1")
        vm.rotationDetail.until { it.rotation != null }
        vm.openRotationAddressChange()
        vm.rotationDetail.until { it.addressChange.addresses.isNotEmpty() }
        vm.pickRotationChangeAddressForAll("office")
        vm.saveRotationAddressChange()
        val s = vm.rotationDetail.until { it.addressChange.error != null }
        assertEquals(AddressProblem("outside", "Green Bowl", "lunch", 3), s.addressChange.problem)
        assertEquals("home", s.rotation!!.deliveryAddresses.single().addressId, "nothing changed")
        assertTrue(requests("me/addresses/coverage").isEmpty(), "rotations leave the four kitchens' check to the server")
    }

    @Test
    fun addingAnAddressFromTheChangeSheetPicksItAndChecksCoverage() = runTest {
        val vm = vm { req ->
            val path = req.url.encodedPath
            when {
                path.endsWith("me/subscriptions/s1") -> respond(detail("home") + "}", HttpStatusCode.OK, json)
                path.endsWith("me/addresses/coverage") -> {
                    val body = Json.parseToJsonElement((req.body as OutgoingContent.ByteArrayContent).bytes().decodeToString()).jsonObject
                    val ids = body["address_ids"]!!.jsonArray.map { it.jsonPrimitive.content }
                    val items = ids.map { a -> """{"branch_id":"b1","address_id":"$a","delivers":${a != "far" && a != "gym"}}""" }
                    respond(items.joinToString(",", "{\"items\":[", "]}"), HttpStatusCode.OK, json)
                }
                path.endsWith("me/addresses") && req.method == HttpMethod.Post -> respond(
                    """{"id":"gym","label":"Gym","city":"Dubai","point":{"lat":25.3,"lng":55.4}}""", HttpStatusCode.Created, json,
                )
                path.endsWith("me/addresses") -> respond(addresses, HttpStatusCode.OK, json)
                else -> respond("""{"items":[]}""", HttpStatusCode.OK, json)
            }
        }
        vm.openSubscription("s1")
        vm.sub.until { it.subscription != null }
        vm.openAddressChange()
        vm.sub.until { it.addressChange.addresses.isNotEmpty() && it.addressChange.outsideAddresses.isNotEmpty() }
        vm.addChangeAddress("Gym", "", "Marina", "Dubai", ae.marketplace.mobile.model.LatLng(25.3, 55.4))
        val s = vm.sub.until { it.addressChange.addressAdded == 1 && it.addressChange.outside("gym") }
        assertEquals(1, requests("me/addresses").count { it.method == HttpMethod.Post })
        assertTrue(s.addressChange.addresses.any { it.id == "gym" }, "the new address joins the list")
        assertEquals("gym", s.addressChange.picked("lunch"), "all meals follow the new address")
        assertEquals("gym", s.addressChange.picked("dinner"))
        assertFalse(s.addressChange.busy)
        assertNull(s.addressChange.addError)
    }

    @Test
    fun rotationChangeMarksAddressesTheRemainingWeeksKitchensDoNotCover() = runTest {
        fun week(n: Int, branch: String, start: String, end: String) =
            """{"week":$n,"starts_on":"$start","ends_on":"$end","branch_id":"$branch","kitchen_name":"K$branch","plan_id":"p$branch","price_minor":1}"""
        val rotation = """{"id":"r2","status":"active","start_date":"2020-01-05","end_date":"2099-02-01","amount_minor":4,
            "weeks":[${week(1, "k1", "2020-01-05", "2020-01-11")},${week(2, "k2", "2099-01-12", "2099-01-18")},
                     ${week(3, "k3", "2099-01-19", "2099-01-25")},${week(4, "k4", "2099-01-26", "2099-02-01")}],
            "can_change_address":true,"delivery_addresses":[{"meal_slot":"lunch","address_id":"home","label":"Home","line":"","effective_from":null}]}"""
        val vm = vm { req ->
            val path = req.url.encodedPath
            when {
                path.endsWith("me/rotations/r2") -> respond(rotation, HttpStatusCode.OK, json)
                path.endsWith("me/addresses") -> respond(addresses, HttpStatusCode.OK, json)
                path.endsWith("me/addresses/coverage") -> {
                    val body = Json.parseToJsonElement((req.body as OutgoingContent.ByteArrayContent).bytes().decodeToString()).jsonObject
                    val branches = body["branch_ids"]!!.jsonArray.map { it.jsonPrimitive.content }
                    val ids = body["address_ids"]!!.jsonArray.map { it.jsonPrimitive.content }
                    val items = branches.flatMap { k -> ids.map { a -> """{"branch_id":"$k","address_id":"$a","delivers":${!(k == "k3" && a == "office") && a != "far"}}""" } }
                    respond(items.joinToString(",", "{\"items\":[", "]}"), HttpStatusCode.OK, json)
                }
                else -> respond("""{"items":[]}""", HttpStatusCode.OK, json)
            }
        }
        vm.openRotation("r2")
        vm.rotationDetail.until { it.rotation != null }
        vm.openRotationAddressChange()
        val s = vm.rotationDetail.until { it.addressChange.outsideAddresses.isNotEmpty() }
        val asked = requests("me/addresses/coverage").single().body!!["branch_ids"]!!.jsonArray.map { it.jsonPrimitive.content }
        assertEquals(listOf("k2", "k3", "k4"), asked, "only the weeks still to come are checked")
        assertEquals("k3", s.addressChange.outsideKitchen("office"), "the week-3 kitchen is named for the office")
        assertEquals("k2", s.addressChange.outsideKitchen("far"), "the first refusing kitchen is named")
        assertFalse(s.addressChange.outside("home"))
    }

    @Test
    fun companyLunchAndMissingAddressRefusalsAreWorded() = runTest {
        val vm = vm { req ->
            val path = req.url.encodedPath
            when {
                path.endsWith("me/subscriptions/s1") -> respond(detail("home") + "}", HttpStatusCode.OK, json)
                path.endsWith("me/addresses") -> respond(addresses, HttpStatusCode.OK, json)
                path.endsWith("subscriptions/s1/addresses") -> respond(
                    """{"title":"Not found","status":404,"code":"address_not_found"}""", HttpStatusCode.NotFound, json,
                )
                else -> respond("""{"items":[]}""", HttpStatusCode.OK, json)
            }
        }
        vm.openSubscription("s1")
        vm.sub.until { it.subscription != null }
        vm.openAddressChange()
        vm.sub.until { it.addressChange.addresses.isNotEmpty() }
        vm.pickChangeAddressForAll("office")
        vm.saveAddressChange()
        val s = vm.sub.until { it.addressChange.error != null }
        assertEquals("gone", s.addressChange.problem?.kind)
        assertNotEquals(Failure.NotFound, s.addressChange.error, "a gone address is not the plan being gone")
    }
}
