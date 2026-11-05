package ae.marketplace.mobile.support

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
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeout
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertIs
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class SupportInvoicesTest {
    private val json = headersOf(HttpHeaders.ContentType, ContentType.Application.Json.toString())
    private val paths = mutableListOf<String>()
    private var invoiceFails = false
    private var listPage = 1

    private val document = """{"id":"i1","number":"AE-INV-2026-000001","kind":"tax_invoice","status":"issued",
        "country":"AE","currency":"AED",
        "supplier":{"name":"Spice Route Kitchen","trn":"100000000000003","address":"Al Barsha, Dubai"},
        "buyer":{"name":"Aisha","trn":"","address":""},
        "supply_date":"2026-11-01","issued_at":"2026-11-01T08:00:00Z","corrects_number":null,
        "net_minor":2000,"tax_minor":100,"gross_minor":2100,
        "lines":[{"description":"Lunch","description_ar":"غداء","quantity":2,"unit_net_minor":1000,
                  "net_minor":2000,"tax_rate_bp":500,"tax_minor":100,"gross_minor":2100}]}"""

    private fun summary(id: String) = """{"id":"$id","number":"N-$id","kind":"simplified",
        "total":{"amount":1050,"currency":"AED"},"supply_date":"2026-11-01",
        "issued_at":"2026-11-01T08:00:00Z","status":"issued"}"""

    private fun api() = SupportApi(
        ApiClientFactory.create(
            baseUrl = "https://api.test/v1",
            tokens = InMemoryTokenStore(),
            identity = ViewerIdentity(deviceId = "test-device"),
            engine = MockEngine { req ->
                val path = req.url.encodedPath
                paths += path
                when {
                    path.endsWith("/me/invoices") -> {
                        val items = if (listPage == 1) summary("a") else summary("b") + "," + summary("a")
                        respond("""{"items":[$items],"next_cursor":null}""", HttpStatusCode.OK, json)
                    }
                    path.endsWith("/me/invoices/missing") ->
                        respond("""{"title":"Not found","status":404}""", HttpStatusCode.NotFound, json)
                    invoiceFails ->
                        respond("""{"title":"boom","status":500}""", HttpStatusCode.InternalServerError, json)
                    else -> respond(document, HttpStatusCode.OK, json)
                }
            },
        ),
    )

    @BeforeTest fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @AfterTest fun tearDown() = Dispatchers.resetMain()

    /** MockEngine answers off the test scheduler, so wait on real time. */
    private suspend fun <T> StateFlow<T>.await(predicate: (T) -> Boolean): T =
        withContext(Dispatchers.Default) { withTimeout(5_000) { first(predicate) } }

    @Test
    fun decodesTheStoredInvoice() = runTest {
        val d = api().invoice("i1")
        assertEquals("/v1/me/invoices/i1", paths.single())
        assertEquals("100000000000003", d.supplier.trn)
        assertEquals(2100L, d.grossMinor)
        assertNull(d.correctsNumber)
        val line = d.lines.single()
        assertEquals("5", line.ratePercent)
        assertEquals("2", line.quantityText)
        assertEquals("غداء", line.label(arabic = true))
        assertEquals("Lunch", line.label(arabic = false))
    }

    @Test
    fun lineFormatting() {
        val l = InvoiceLine("Lunch", "", 1.5, 1000, 1500, 1250, 187, 1687)
        assertEquals("12.5", l.ratePercent)
        assertEquals("1.5", l.quantityText)
        // No Arabic description falls back to English.
        assertEquals("Lunch", l.label(arabic = true))
        assertEquals("0", l.copy(taxRateBp = 0).ratePercent)
    }

    @Test
    fun openInvoiceLoadsFailsAndRetries() = runTest {
        val vm = SupportViewModel(api())
        invoiceFails = true
        vm.openInvoice("i1")
        val failed = vm.invoice.await { it.error != null }
        assertIs<Failure.Server>(failed.error)
        assertNull(failed.invoice)

        invoiceFails = false
        vm.retryInvoice()
        val ok = vm.invoice.await { it.invoice != null }
        assertEquals("AE-INV-2026-000001", ok.invoice?.number)
        assertNull(ok.error)
    }

    @Test
    fun someoneElsesInvoiceIsNotFound() = runTest {
        val vm = SupportViewModel(api())
        vm.openInvoice("missing")
        assertIs<Failure.NotFound>(vm.invoice.await { it.error != null }.error)
    }

    @Test
    fun forcedReloadKeepsTheListAndPicksUpNewInvoices() = runTest {
        val vm = SupportViewModel(api())
        vm.loadInvoices()
        assertEquals(listOf("a"), vm.invoices.await { it.loaded }.items.map { it.id })

        // Without force a second visit does not refetch.
        vm.loadInvoices()
        assertEquals(1, paths.size)

        listPage = 2
        vm.loadInvoices(force = true)
        // The old list stays on screen while it refreshes.
        val during = vm.invoices.value
        if (during.refreshing) assertEquals(listOf("a"), during.items.map { it.id })
        assertTrue(during.loaded)
        val fresh = vm.invoices.await { it.items.size == 2 && !it.refreshing }
        assertEquals(listOf("b", "a"), fresh.items.map { it.id })
        assertEquals(2, paths.size)
    }
}
