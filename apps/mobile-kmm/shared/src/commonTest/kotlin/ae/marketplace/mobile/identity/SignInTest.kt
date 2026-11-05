package ae.marketplace.mobile.identity

import ae.marketplace.mobile.network.ApiClientFactory
import ae.marketplace.mobile.network.InMemoryTokenStore
import ae.marketplace.mobile.network.Tokens
import io.ktor.client.HttpClient
import io.ktor.client.engine.mock.MockEngine
import io.ktor.client.engine.mock.MockRequestHandleScope
import io.ktor.client.engine.mock.respond
import io.ktor.client.request.HttpRequestData
import io.ktor.client.request.HttpResponseData
import io.ktor.client.request.get
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.HttpStatusCode
import io.ktor.http.headersOf
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlin.test.AfterTest
import kotlin.test.BeforeTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class SignInTest {

    @BeforeTest
    fun main() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @AfterTest
    fun reset() = Dispatchers.resetMain()

    /** The engine answers off the main dispatcher, so wait for the request to land. */
    private suspend fun SignInViewModel.settle() = state.first { !it.busy }

    private val json = headersOf(HttpHeaders.ContentType, ContentType.Application.Json.toString())

    private fun client(
        tokens: InMemoryTokenStore,
        handler: suspend MockRequestHandleScope.(HttpRequestData) -> HttpResponseData,
    ): HttpClient = ApiClientFactory.create(
        baseUrl = "https://api.test/v1",
        tokens = tokens,
        identity = ViewerIdentity(deviceId = "test-device"),
        engine = MockEngine(handler),
    )

    private fun MockRequestHandleScope.problem(status: Int, code: String? = null) = respond(
        """{"type":"about:blank","title":"x","status":$status${code?.let { ",\"code\":\"$it\"" } ?: ""}}""",
        HttpStatusCode.fromValue(status),
        headersOf(HttpHeaders.ContentType, "application/problem+json"),
    )

    @Test
    fun newSignInReplacesTheCachedBearer() = runTest {
        val tokens = InMemoryTokenStore(Tokens("old-access", "old-refresh"))
        val seen = mutableListOf<String?>()
        val client = client(tokens) { req ->
            when (req.url.encodedPath) {
                "/v1/auth/otp/verify" -> respond(
                    """{"access_token":"new-access","refresh_token":"new-refresh"}""",
                    HttpStatusCode.OK, json,
                )
                else -> {
                    seen += req.headers[HttpHeaders.Authorization]
                    respond("{}", HttpStatusCode.OK, json)
                }
            }
        }
        val api = SignInApi(client, tokens)

        client.get("me")
        api.verify("c1", "123456")
        client.get("me")
        api.signOut()
        client.get("me")

        assertEquals("Bearer old-access", seen[0])
        assertEquals("Bearer new-access", seen[1])
        assertNull(seen[2], "a signed-out client must not send the previous token")
    }

    @Test
    fun cooldownComesFromTheServer() = runTest {
        val tokens = InMemoryTokenStore()
        val client = client(tokens) {
            respond(
                """{"challenge_id":"c1","expires_at":"2030-01-01T00:00:00Z","resend_after_seconds":60}""",
                HttpStatusCode.Accepted, json,
            )
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        vm.onPhone("0501234567")
        vm.sendCode(nowMillis = 1_000)
        vm.settle()
        assertEquals("c1", vm.state.value.challengeId)
        assertEquals(61_000, vm.state.value.resendAtMillis)
    }

    private suspend fun problemFor(
        status: Int,
        code: String? = null,
        resend: Boolean = false,
    ): SignInProblem? {
        val tokens = InMemoryTokenStore()
        var first = true
        val client = client(tokens) {
            if (resend && first) {
                first = false
                respond(
                    """{"challenge_id":"c1","resend_after_seconds":0}""",
                    HttpStatusCode.Accepted, json,
                )
            } else {
                problem(status, code)
            }
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        vm.onPhone("+971501234567")
        if (resend) {
            vm.sendCode(0)
            vm.settle()
            assertEquals("c1", vm.state.value.challengeId)
        }
        vm.sendCode(0)
        vm.settle()
        if (resend) assertEquals("c1", vm.state.value.challengeId)
        return vm.state.value.error
    }

    @Test
    fun rateLimitIsNotABadNumber() = runTest {
        assertEquals(SignInProblem.TooMany, problemFor(429, "otp_rate_limited"))
        assertEquals(SignInProblem.TooMany, problemFor(409))
        assertEquals(SignInProblem.BadPhone, problemFor(422))
    }

    @Test
    fun failedResendKeepsTheChallenge() = runTest {
        assertEquals(SignInProblem.ResendTooMany, problemFor(429, "otp_rate_limited", resend = true))
        assertEquals(SignInProblem.ResendFailed, problemFor(503, resend = true))
    }

    @Test
    fun suspendedAccountIsNotAWrongCode() = runTest {
        val tokens = InMemoryTokenStore()
        var calls = 0
        val client = client(tokens) {
            if (calls++ == 0) {
                respond("""{"challenge_id":"c1","resend_after_seconds":60}""", HttpStatusCode.Accepted, json)
            } else {
                problem(403)
            }
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        vm.onPhone("+971501234567")
        vm.sendCode(0)
        vm.settle()
        vm.onCode("123456")
        vm.verify()
        vm.settle()
        assertEquals(SignInProblem.AccountBlocked, vm.state.value.error)
        assertEquals("c1", vm.state.value.challengeId)
    }

    @Test
    fun thePickedCountryIsSentWithTheNumber() = runTest {
        val tokens = InMemoryTokenStore()
        var body = ""
        val client = client(tokens) { req ->
            body = (req.body as io.ktor.http.content.TextContent).text
            respond("""{"challenge_id":"c1","resend_after_seconds":60}""", HttpStatusCode.Accepted, json)
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        assertEquals(PhoneCountry.AE, vm.state.value.country)
        vm.onCountry(PhoneCountry.SA)
        vm.onPhone("0551230003")
        vm.sendCode(0)
        vm.settle()
        assertTrue(""""country":"SA"""" in body, body)
        assertTrue(""""phone":"0551230003"""" in body, body)
        assertEquals("+966 551230003", vm.state.value.sentTo)
    }

    @Test
    fun wrongLengthForTheCountryIsItsOwnProblem() = runTest {
        val tokens = InMemoryTokenStore()
        val client = client(tokens) {
            respond(
                """{"type":"about:blank","title":"x","status":422,"errors":[{"field":"phone","code":"wrong_length"}]}""",
                HttpStatusCode.UnprocessableEntity,
                headersOf(HttpHeaders.ContentType, "application/problem+json"),
            )
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        vm.onCountry(PhoneCountry.QA)
        vm.onPhone("0551230003")
        vm.sendCode(0)
        vm.settle()
        assertEquals(SignInProblem.WrongLength, vm.state.value.error)
    }

    @Test
    fun rateLimitCarriesRetryAfter() = runTest {
        val tokens = InMemoryTokenStore()
        val client = client(tokens) {
            respond(
                """{"type":"about:blank","title":"x","status":429,"code":"otp_rate_limited"}""",
                HttpStatusCode.TooManyRequests,
                headersOf(
                    HttpHeaders.ContentType to listOf("application/problem+json"),
                    HttpHeaders.RetryAfter to listOf("1500"),
                ),
            )
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        vm.onPhone("+971501234567")
        vm.sendCode(0)
        vm.settle()
        assertEquals(SignInProblem.TooMany, vm.state.value.error)
        assertEquals(1500, vm.state.value.retryAfterSeconds)
        assertEquals(25, vm.state.value.retryAfterMinutes)
    }

    @Test
    fun aRetiredCodeSendsTheViewerToTheLatestMessage() = runTest {
        val tokens = InMemoryTokenStore()
        var calls = 0
        val client = client(tokens) {
            if (calls++ == 0) {
                respond("""{"challenge_id":"c1","resend_after_seconds":60}""", HttpStatusCode.Accepted, json)
            } else {
                problem(401, "otp_superseded")
            }
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        vm.onPhone("+971501234567")
        vm.sendCode(0)
        vm.settle()
        vm.onCode("123456")
        vm.verify()
        vm.settle()
        assertEquals(SignInProblem.Superseded, vm.state.value.error)
        assertEquals("", vm.state.value.challengeId)
    }

    @Test
    fun verifySendsTheDeviceBlock() = runTest {
        val tokens = InMemoryTokenStore()
        var body = ""
        val client = client(tokens) { req ->
            body = (req.body as io.ktor.http.content.TextContent).text
            respond("""{"access_token":"a","refresh_token":"r"}""", HttpStatusCode.OK, json)
        }
        SignInApi(client, tokens, SignInDevice(platform = "android", name = "Pixel 8")).verify("c1", "123456")
        assertTrue(""""device":{"platform":"android","name":"Pixel 8"}""" in body, body)
    }

    @Test
    fun theCooldownOnlyHoldsForTheSameNumber() = runTest {
        val tokens = InMemoryTokenStore()
        val sent = mutableListOf<String>()
        val client = client(tokens) { req ->
            sent += (req.body as io.ktor.http.content.TextContent).text
            respond("""{"challenge_id":"c${sent.size}","resend_after_seconds":60}""", HttpStatusCode.Accepted, json)
        }
        val vm = SignInViewModel(SignInApi(client, tokens), Session(tokens))
        vm.onPhone("0501234567")
        vm.sendCode(nowMillis = 1_000)
        vm.settle()
        vm.changeNumber()

        // Same number, typed differently: still cooling down.
        vm.onPhone("971501234567")
        vm.sendCode(nowMillis = 2_000)
        vm.settle()
        assertEquals(1, sent.size)

        vm.onPhone("0509999999")
        vm.sendCode(nowMillis = 3_000)
        vm.settle()
        assertEquals(2, sent.size)
        assertEquals("c2", vm.state.value.challengeId)
        assertEquals(63_000, vm.state.value.resendAtMillis)
    }
}
