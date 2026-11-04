package ae.marketplace.mobile.meals

import ae.marketplace.mobile.identity.ViewerIdentity
import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.InMemoryTokenStore
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.client.request.HttpRequestData
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class MealsStoreTest {
    private val json = headersOf(HttpHeaders.ContentType, "application/json")
    private val kitchens = """{"items":[{"branch_id":"b1","name":"Teta","from_meal_price_minor":3000,"currency":"AED"}]}"""

    @BeforeTest fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())
    @AfterTest fun tearDown() = Dispatchers.resetMain()

    private fun vm(handler: suspend io.ktor.client.engine.mock.MockRequestHandleScope.(HttpRequestData) -> io.ktor.client.request.HttpResponseData) =
        MealsViewModel(
            MealsApi(
                ApiClientFactory.create(
                    baseUrl = "https://api.test/v1",
                    tokens = InMemoryTokenStore(),
                    identity = ViewerIdentity(deviceId = "test-device"),
                    engine = MockEngine(handler),
                ),
            ),
        )

    /** MockEngine answers off the test scheduler, so wait on real time for the load to land. */
    private suspend fun settled(vm: MealsViewModel) = withContext(Dispatchers.Default) {
        withTimeout(5_000) {
            vm.store.first { !it.loading && (it.error != null || (it.loadedFor != null && !it.stale)) }
        }
    }

    @Test
    fun merchandisingFailureStillShowsKitchens() = runTest {
        val vm = vm { req ->
            when {
                req.url.encodedPath.endsWith("meals/kitchens") -> respond(kitchens, HttpStatusCode.OK, json)
                else -> respond("""{"title":"boom"}""", HttpStatusCode.InternalServerError, json)
            }
        }
        vm.setLocation(25.0, 55.0)
        advanceUntilIdle()
        val s = settled(vm)
        assertNull(s.error)
        assertEquals(listOf("b1"), s.kitchens.map { it.branchId })
        assertTrue(s.sections.isEmpty())
    }

    @Test
    fun failedRefetchForANewPlaceDropsTheOldList() = runTest {
        var fail = false
        val vm = vm { req ->
            when {
                req.url.encodedPath.endsWith("meals/kitchens") && fail ->
                    respond("""{"title":"down"}""", HttpStatusCode.ServiceUnavailable, json)
                req.url.encodedPath.endsWith("meals/kitchens") -> respond(kitchens, HttpStatusCode.OK, json)
                req.url.encodedPath.endsWith("meals/home") -> respond("""{"sections":null}""", HttpStatusCode.OK, json)
                else -> respond("""{"items":[]}""", HttpStatusCode.OK, json)
            }
        }
        vm.setLocation(25.0, 55.0)
        advanceUntilIdle()
        assertEquals(1, settled(vm).kitchens.size)

        fail = true
        vm.setLocation(24.4, 54.4)
        advanceUntilIdle()
        val s = settled(vm)
        assertNotNull(s.error)
        assertTrue(s.kitchens.isEmpty(), "kitchens for the old place must not stand in for the new one")
    }

    @Test
    fun searchAndFiltersReachTheApi() = runTest {
        val seen = mutableListOf<io.ktor.http.Url>()
        val vm = vm { req ->
            if (req.url.encodedPath.endsWith("meals/kitchens")) seen += req.url
            respond(if (req.url.encodedPath.endsWith("meals/kitchens")) kitchens else """{"items":[]}""", HttpStatusCode.OK, json)
        }
        vm.setLocation(25.0, 55.0)
        vm.selectCuisine("lebanese")
        vm.setRotationOnly(true)
        vm.selectSlot("iftar")
        vm.setQuery("mujaddara")
        advanceUntilIdle()
        settled(vm)
        val last = seen.last().parameters
        assertEquals("mujaddara", last["q"])
        assertEquals("lebanese", last["cuisine"])
        assertEquals("true", last["rotation"])
        assertEquals("iftar", last["slot"])
        assertTrue(vm.store.value.filtering)
    }
}
