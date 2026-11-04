package ae.marketplace.mobile.meals

import ae.marketplace.mobile.identity.ViewerIdentity
import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.InMemoryTokenStore
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertNotNull
import kotlin.test.assertNull

@OptIn(ExperimentalCoroutinesApi::class)
class KitchenReviewsTest {
    private val json = headersOf(HttpHeaders.ContentType, ContentType.Application.Json.toString())
    private var reviewsFail = true

    private fun vm() = MealsViewModel(
        MealsApi(
            ApiClientFactory.create(
                baseUrl = "https://api.test/v1",
                tokens = InMemoryTokenStore(),
                identity = ViewerIdentity(deviceId = "test-device"),
                engine = MockEngine { request ->
                    val path = request.url.encodedPath
                    when {
                        path.endsWith("/reviews") && reviewsFail ->
                            respond("""{"title":"boom","status":500}""", HttpStatusCode.InternalServerError, json)
                        path.endsWith("/reviews") ->
                            respond("""{"items":[{"id":"r1","rating":5,"comment":"Lovely","reply":"Thanks!"}]}""", HttpStatusCode.OK, json)
                        path.endsWith("/offers") ->
                            respond(
                                """{"items":[
                                  {"id":"o1","name":"Welcome","discount_type":"percent","percent_bp":2000,"amount_minor":0,"currency":"AED","applies_to":"both","audience":"new_customers"},
                                  {"id":"o2","name":"Rotation only","discount_type":"fixed","percent_bp":0,"amount_minor":500,"currency":"AED","applies_to":"rotations","audience":"everyone"}
                                ]}""",
                                HttpStatusCode.OK,
                                json,
                            )
                        else -> respond("""{"branch_id":"b1","name":"Teta"}""", HttpStatusCode.OK, json)
                    }
                },
            ),
        ),
    )

    @BeforeTest fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @AfterTest fun tearDown() = Dispatchers.resetMain()

    @Test
    fun reviewsFailureIsExposedAndRetryable() = runTest {
        val vm = vm()
        vm.setLocation(25.08, 55.14)
        vm.openKitchen("b1")
        // The mock engine answers off the test scheduler, so wait on the state itself.
        val failed = vm.kitchen.first { it.reviewsError != null }
        assertNotNull(failed.kitchen)
        assertNull(failed.error)
        assertIs<Failure.Server>(failed.reviewsError)
        assertEquals(emptyList(), failed.reviews)

        reviewsFail = false
        vm.retryReviews()
        val ok = vm.kitchen.first { it.reviews.isNotEmpty() }
        assertNull(ok.reviewsError)
        assertEquals("Thanks!", ok.reviews.single().reply)
    }

    @Test
    fun offersSkipRotationOnlyOnes() = runTest {
        val vm = vm()
        vm.setLocation(25.08, 55.14)
        vm.openKitchen("b1")
        val offers = vm.kitchen.first { it.offers.isNotEmpty() }.offers
        assertEquals(listOf("o1"), offers.map { it.id })
        assertEquals(true, offers.single().newCustomersOnly)
    }
}
