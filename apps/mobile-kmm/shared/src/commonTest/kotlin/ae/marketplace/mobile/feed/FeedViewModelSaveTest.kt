package ae.marketplace.mobile.feed

import ae.marketplace.mobile.events.EventQueue
import ae.marketplace.mobile.events.EventSender
import ae.marketplace.mobile.identity.ViewerIdentity
import ae.marketplace.mobile.model.ClientEvent
import ae.marketplace.mobile.model.EventBatch
import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.InMemoryTokenStore
import ae.marketplace.mobile.playback.PlaybackCoordinator
import ae.marketplace.mobile.playback.ReelPlayer
import ae.marketplace.mobile.playback.ReelPlayerPool
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.respond
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpMethod
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

/**
 * A decoder-free player, so the coordinator the view model owns can be built.
 *
 * The view model never touches playback in any of these tests. It is a
 * constructor argument, and a fake that does nothing is the honest way to say
 * so — a mock with expectations here would assert something this file is not
 * about.
 */
private class SilentPlayer : ReelPlayer {
    private var url: String? = null
    override fun prepare(url: String) { this.url = url }
    override fun play() {}
    override fun pause() {}
    override fun positionMs(): Long = 0
    override fun isReady(): Boolean = url != null
    override fun release() { url = null }
    override fun currentUrl(): String? = url
}

/**
 * The bookmark, end to end through the view model.
 *
 * Three properties, and each one is a bug that shipped in some other app: the
 * flip is immediate rather than waiting for the network, the write actually
 * goes out with the right verb, and a failed write puts the state back rather
 * than leaving a bookmark that a restart will erase.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class FeedViewModelSaveTest {

    private val dispatcher = StandardTestDispatcher()

    @BeforeTest
    fun installMain() {
        // viewModelScope dispatches on Main, which does not exist in a unit
        // test until it is told to.
        Dispatchers.setMain(dispatcher)
    }

    @AfterTest
    fun removeMain() = Dispatchers.resetMain()

    private fun page(id: String, saved: Boolean, saves: Long) =
        """{"items":[{"id":"$id","author":{"type":"vendor_branch","id":"b1"},
           "renditions":[],"published_at":"2026-07-28T10:00:00Z",
           "counters":{"saves":$saves},"viewer":{"saved":$saved}}],
           "next_cursor":null,"ranking_profile":"phase-4-heuristic"}"""

    private val jsonHeaders =
        headersOf(HttpHeaders.ContentType, ContentType.Application.Json.toString())

    /**
     * The view model and the calls that reached the wire.
     *
     * [saveCalls] records method and path rather than a bare count, because the
     * failure this guards against is `toggleSave` sending PUT twice — an unsave
     * that re-saves is indistinguishable from a save that never happened, until
     * the next refresh.
     */
    private class Harness(
        scope: TestScope,
        body: String,
        private val saveStatus: HttpStatusCode,
        jsonHeaders: io.ktor.http.Headers,
    ) {
        /**
         * The save write, once it reaches the wire.
         *
         * A deferred rather than a list, because the engine answers on its own
         * dispatcher and the test scheduler cannot be advanced past a
         * suspension it does not own. Awaiting the call is also the assertion:
         * a `toggleSave` that never wrote would hang here rather than pass.
         */
        val saveHit = CompletableDeferred<Pair<HttpMethod, String>>()
        val sent = mutableListOf<ClientEvent>()

        private val api = FeedApi(
            ApiClientFactory.create(
                baseUrl = "https://api.test/v1",
                tokens = InMemoryTokenStore(),
                identity = ViewerIdentity(deviceId = "test-device"),
                engine = MockEngine { request ->
                    if (request.url.encodedPath.endsWith("/save")) {
                        saveHit.complete(request.method to request.url.encodedPath)
                        respond("", saveStatus)
                    } else {
                        respond(body, HttpStatusCode.OK, jsonHeaders)
                    }
                },
            )
        )

        val queue = EventQueue(
            sender = EventSender { batch: EventBatch -> sent += batch.events },
            sessionId = "s1",
            timestampProvider = { "2026-07-28T12:00:00Z" },
            scope = scope,
        )

        /**
         * Suspends until the first page has landed.
         *
         * Not `advanceUntilIdle`: the mock engine answers on its own
         * dispatcher, so there is nothing on the test scheduler to advance
         * while the request is in flight and the state would still read
         * `loading`.
         */
        suspend fun awaitFeed(): List<ae.marketplace.mobile.model.FeedItem> =
            viewModel.state.first { !it.loading && it.items.isNotEmpty() }.items

        val viewModel = FeedViewModel(
            pager = FeedPager(api),
            playback = PlaybackCoordinator(
                pool = ReelPlayerPool(factory = { SilentPlayer() }),
                events = queue,
                metered = { false },
                now = { 0L },
            ),
            events = queue,
            api = api,
        )
    }

    @Test
    fun save_flips_before_the_write_and_sends_a_put() = runTest(dispatcher) {
        val h = Harness(this, page("reel-1", saved = false, saves = 0), HttpStatusCode.NoContent, jsonHeaders)
        h.viewModel.refresh()
        val before = h.awaitFeed().single()
        assertFalse(before.viewer.saved)

        h.viewModel.toggleSave("reel-1")

        // Read back with nothing awaited in between. A view model that waited
        // for the response would still be reporting unsaved here, which is the
        // whole failure this optimistic path exists to avoid.
        val flipped = h.viewModel.state.value.items.single()
        assertTrue(flipped.viewer.saved)
        assertEquals(1L, flipped.counters.saves)

        assertEquals(HttpMethod.Put to "/v1/reels/reel-1/save", h.saveHit.await())
        assertTrue(h.viewModel.state.value.items.single().viewer.saved)

        advanceUntilIdle()
        h.queue.flush()
        val event = h.sent.single { it.type == ClientEvent.SAVE }
        assertEquals("reel-1", event.reelId)
    }

    @Test
    fun unsave_sends_a_delete_and_takes_the_count_back_down() = runTest(dispatcher) {
        val h = Harness(this, page("reel-1", saved = true, saves = 5), HttpStatusCode.NoContent, jsonHeaders)
        h.viewModel.refresh()
        assertTrue(h.awaitFeed().single().viewer.saved)

        h.viewModel.toggleSave("reel-1")
        val flipped = h.viewModel.state.value.items.single()
        assertFalse(flipped.viewer.saved)
        assertEquals(4L, flipped.counters.saves)

        assertEquals(HttpMethod.Delete to "/v1/reels/reel-1/save", h.saveHit.await())

        advanceUntilIdle()
        h.queue.flush()
        assertEquals(ClientEvent.UNSAVE, h.sent.single { it.reelId == "reel-1" }.type)
    }

    @Test
    fun a_failed_write_puts_the_bookmark_back() = runTest(dispatcher) {
        val h = Harness(
            this,
            page("reel-1", saved = false, saves = 3),
            HttpStatusCode.InternalServerError,
            jsonHeaders,
        )
        h.viewModel.refresh()
        h.awaitFeed()

        h.viewModel.toggleSave("reel-1")
        assertTrue(h.viewModel.state.value.items.single().viewer.saved)

        // Back where it started. A bookmark that survives the failed write is a
        // reel the viewer will look for in Saved and not find.
        val reverted = h.viewModel.state
            .first { !(it.items.singleOrNull()?.viewer?.saved ?: true) }
            .items.single()
        assertFalse(reverted.viewer.saved)
        assertEquals(3L, reverted.counters.saves)
    }

    @Test
    fun a_reel_that_is_not_on_screen_is_left_alone() = runTest(dispatcher) {
        val h = Harness(this, page("reel-1", saved = false, saves = 0), HttpStatusCode.NoContent, jsonHeaders)
        h.viewModel.refresh()
        h.awaitFeed()

        h.viewModel.toggleSave("reel-does-not-exist")
        advanceUntilIdle()

        assertFalse(h.saveHit.isCompleted)
        assertFalse(h.viewModel.state.value.items.single().viewer.saved)
    }
}
