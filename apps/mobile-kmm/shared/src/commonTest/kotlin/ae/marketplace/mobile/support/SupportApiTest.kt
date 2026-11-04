package ae.marketplace.mobile.support

import ae.marketplace.mobile.identity.ViewerIdentity
import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.InMemoryTokenStore
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class SupportApiTest {

    private val json = headersOf(HttpHeaders.ContentType, "application/json")

    private fun api(engine: MockEngine) = SupportApi(
        ApiClientFactory.create(
            baseUrl = "https://api.test/v1",
            tokens = InMemoryTokenStore(),
            identity = ViewerIdentity(deviceId = "test-device"),
            engine = engine,
        )
    )

    @Test
    fun sendsTheCallersIdempotencyKey() = runTest {
        val keys = mutableListOf<String?>()
        val engine = MockEngine { req ->
            keys += req.headers["Idempotency-Key"]
            respond(
                """{"id":"t1","subject":"Cold","status":"open","updated_at":"2026-11-05T01:00:00Z","messages":[]}""",
                HttpStatusCode.Created, json,
            )
        }
        val api = api(engine)
        api.open("Cold", "Lunch arrived cold", "k-1")
        api.open("Cold", "Lunch arrived cold", "k-1")
        api.reply("t1", "Thanks", "k-2")
        assertEquals(listOf<String?>("k-1", "k-1", "k-2"), keys)
    }

    @Test
    fun pagesTicketsAndInvoicesWithTheCursor() = runTest {
        val urls = mutableListOf<String>()
        val engine = MockEngine { req ->
            urls += req.url.encodedPath + "?" + req.url.encodedQuery
            val body = if (req.url.encodedPath.endsWith("invoices")) {
                """{"items":[{"id":"i1","number":"AE-INV-2026-000001","kind":"simplified",
                   "total":{"amount":1050,"currency":"AED"},"tax":{"amount":50,"currency":"AED"},
                   "supply_date":"2026-11-01","issued_at":"2026-11-01T08:00:00Z","status":"issued"}],
                   "next_cursor":null}"""
            } else {
                """{"items":[{"id":"t1","subject":"Cold","status":"answered","subscription_id":null,
                   "created_at":"2026-11-01T08:00:00Z","updated_at":"2026-11-02T08:00:00Z"}],
                   "next_cursor":"abc"}"""
            }
            respond(body, HttpStatusCode.OK, json)
        }
        val api = api(engine)
        val tickets = api.tickets()
        assertEquals("abc", tickets.nextCursor)
        api.tickets(tickets.nextCursor)
        val invoices = api.invoices()
        assertEquals(1050L, invoices.items.single().total.amount)
        assertNull(invoices.nextCursor)
        assertEquals("/v1/me/support/tickets?", urls[0])
        assertEquals("/v1/me/support/tickets?cursor=abc", urls[1])
    }
}
