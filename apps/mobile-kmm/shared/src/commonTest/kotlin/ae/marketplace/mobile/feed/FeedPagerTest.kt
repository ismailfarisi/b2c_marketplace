package ae.marketplace.mobile.feed

import ae.marketplace.mobile.identity.ViewerIdentity
import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.ApiException
import ae.marketplace.mobile.network.InMemoryTokenStore
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.ContentType
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import io.ktor.http.HttpHeaders
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class FeedPagerTest {

    private fun page(ids: List<String>, next: String?) = buildString {
        append("""{"items":[""")
        append(ids.joinToString(",") {
            """{"id":"$it","author":{"type":"vendor_branch","id":"b1"},"renditions":[],"published_at":"2026-07-28T10:00:00Z"}"""
        })
        append("""],"next_cursor":""")
        append(if (next == null) "null" else "\"$next\"")
        append(""","ranking_profile":"phase-4-heuristic"}""")
    }

    /**
     * Built through [ApiClientFactory] rather than as a bare `HttpClient`.
     *
     * The first version of this file constructed its own client, which has no
     * response validator — so every failure arrived as Ktor's
     * `ClientRequestException` and never as an [ApiException]. The stale-cursor
     * test passed while exercising a client that could not produce the error it
     * claimed to recover from. Going through the factory is what makes these
     * tests test the client that ships.
     */
    private fun api(handler: MockEngine.Companion.() -> MockEngine): FeedApi =
        FeedApi(
            ApiClientFactory.create(
                baseUrl = "https://api.test/v1",
                tokens = InMemoryTokenStore(),
                identity = ViewerIdentity(deviceId = "test-device"),
                engine = MockEngine.handler(),
            )
        )

    private fun jsonOk(body: String) =
        headersOf(HttpHeaders.ContentType, ContentType.Application.Json.toString()) to body

    @Test
    fun pages_forward_with_the_opaque_cursor() = runTest {
        val cursors = mutableListOf<String?>()
        val pager = FeedPager(api {
            MockEngine { request ->
                cursors += request.url.parameters["cursor"]
                val body = when (cursors.size) {
                    1 -> page(listOf("a", "b"), "CURSOR-2")
                    else -> page(listOf("c"), null)
                }
                respond(body, HttpStatusCode.OK, jsonOk(body).first)
            }
        })

        pager.refresh(25.2, 55.3)
        pager.loadNext(25.2, 55.3)

        assertEquals(listOf(null, "CURSOR-2"), cursors)
        assertEquals(listOf("a", "b", "c"), pager.snapshot().map { it.id })
    }

    @Test
    fun a_stale_cursor_reloads_from_the_top_rather_than_erroring() = runTest {
        // The API rejects a stale cursor with 422 rather than silently
        // restarting. A client that surfaced that as an error screen would be
        // showing a failure to a user whose only problem is an old page token.
        var call = 0
        val pager = FeedPager(api {
            MockEngine { request ->
                call++
                when {
                    call == 1 -> respond(page(listOf("a"), "OLD"), HttpStatusCode.OK,
                        jsonOk("").first)
                    request.url.parameters["cursor"] == "OLD" -> {
                        val problem = """{"type":"about:blank","title":"Validation failed","status":422,
                            "errors":[{"field":"cursor","code":"stale"}]}"""
                        respond(problem, HttpStatusCode.UnprocessableEntity, jsonOk("").first)
                    }
                    else -> respond(page(listOf("x", "y"), null), HttpStatusCode.OK, jsonOk("").first)
                }
            }
        })

        pager.refresh(25.2, 55.3)
        val fresh = pager.loadNext(25.2, 55.3)

        assertEquals(listOf("x", "y"), fresh.map { it.id })
        assertEquals(listOf("x", "y"), pager.snapshot().map { it.id }, "the stale page is discarded")
    }

    @Test
    fun other_errors_still_propagate() = runTest {
        // Only a *cursor* 422 is special. Swallowing every 422 would hide real
        // validation failures behind a silent reload loop.
        val pager = FeedPager(api {
            MockEngine {
                val problem = """{"type":"about:blank","title":"Validation failed","status":422,
                    "errors":[{"field":"lat","code":"required"}]}"""
                respond(problem, HttpStatusCode.UnprocessableEntity, jsonOk("").first)
            }
        })
        assertFailsWith<ApiException> { pager.refresh(0.0, 0.0) }
    }

    @Test
    fun an_exhausted_feed_stays_exhausted() = runTest {
        // PLAN-VIDEO-FIRST §10: "a feed that can be exhausted — finite and
        // locally relevant beats infinite and irrelevant". A client that looped
        // back to the top would quietly rewrite the product promise.
        var calls = 0
        val pager = FeedPager(api {
            MockEngine {
                calls++
                respond(page(listOf("a"), null), HttpStatusCode.OK, jsonOk("").first)
            }
        })

        pager.refresh(25.2, 55.3)
        assertTrue(pager.isExhausted())

        assertTrue(pager.loadNext(25.2, 55.3).isEmpty())
        assertEquals(1, calls, "an exhausted feed must not keep requesting")
    }

    @Test
    fun duplicate_ids_never_reach_the_list() = runTest {
        // A duplicate id in a lazy list is a crash on Android, not a cosmetic
        // problem. The server already excludes seen reels; this is belt and
        // braces at the cost of a set over one page.
        var call = 0
        val pager = FeedPager(api {
            MockEngine {
                call++
                val body = if (call == 1) page(listOf("a", "b"), "C2") else page(listOf("b", "c"), null)
                respond(body, HttpStatusCode.OK, jsonOk("").first)
            }
        })

        pager.refresh(25.2, 55.3)
        val second = pager.loadNext(25.2, 55.3)

        assertEquals(listOf("c"), second.map { it.id })
        assertEquals(listOf("a", "b", "c"), pager.snapshot().map { it.id })
    }

    @Test
    fun prefetch_triggers_near_the_end_and_not_before() = runTest {
        val pager = FeedPager(api {
            MockEngine { respond(page(List(10) { "r$it" }, "C2"), HttpStatusCode.OK, jsonOk("").first) }
        })
        pager.refresh(25.2, 55.3)

        assertFalse(pager.shouldLoadMore(3), "fetching this early pays for pages nobody reaches")
        assertTrue(pager.shouldLoadMore(7), "leaving it later stalls the feed at the worst moment")
    }

    @Test
    fun the_ranking_profile_is_carried_through() = runTest {
        // "The feed looked wrong at 14:20" should be answerable, not debatable.
        val pager = FeedPager(api {
            MockEngine { respond(page(listOf("a"), null), HttpStatusCode.OK, jsonOk("").first) }
        })
        pager.refresh(25.2, 55.3)
        assertEquals("phase-4-heuristic", pager.rankingProfile)
    }
}
