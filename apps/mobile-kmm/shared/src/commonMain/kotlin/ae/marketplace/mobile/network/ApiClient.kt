package ae.marketplace.mobile.network

import ae.marketplace.mobile.identity.ViewerIdentity
import io.ktor.client.HttpClient
import io.ktor.client.HttpClientConfig
import io.ktor.client.engine.HttpClientEngine
import io.ktor.client.call.body
import io.ktor.client.plugins.DefaultRequest
import io.ktor.client.plugins.HttpResponseValidator
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.ResponseException
import io.ktor.client.plugins.auth.Auth
import io.ktor.client.plugins.auth.providers.BearerTokens
import io.ktor.client.plugins.auth.providers.bearer
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.plugins.defaultRequest
import io.ktor.client.request.header
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.http.ContentType
import io.ktor.http.HttpHeaders
import io.ktor.http.contentType
import io.ktor.serialization.kotlinx.json.json
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * The one HTTP client. Everything else in this module goes through it.
 *
 * Two things are worth reading closely.
 *
 * **Token refresh is Ktor's, not hand-rolled.** The TypeScript client had to
 * hand-write single-flight refresh, because five queries firing on mount against
 * an expired token trigger five concurrent refreshes, four of which use an
 * already-rotated token and are correctly rejected as replay — logging the user
 * out mid-session. Ktor's `Auth` plugin serialises `refreshTokens` for us, which
 * removes the whole class of bug rather than fixing one instance of it.
 *
 * **Errors are thrown as [ApiException] at the client, not at each call site.**
 * `expectSuccess` plus a response validator means no repository has to remember
 * to check a status code, and the problem+json body is parsed once.
 */
object ApiClientFactory {

    val json: Json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }

    /**
     * [engine] is for tests, and taking it here rather than letting a test build
     * its own `HttpClient` is not a convenience — it is the fix for a bug this
     * file's own test suite found.
     *
     * A hand-built client in a test has no [HttpResponseValidator], so every
     * failure arrives as Ktor's `ClientRequestException` instead of an
     * [ApiException]. The stale-cursor recovery in `FeedPager` is written
     * against [ApiException], so it silently never fired — the test proved the
     * recovery worked while testing a client that could not produce the error it
     * recovers from.
     */
    fun create(
        baseUrl: String,
        tokens: TokenStore,
        identity: ViewerIdentity,
        engine: HttpClientEngine? = null,
    ): HttpClient {
        val config: HttpClientConfig<*>.() -> Unit = {
        expectSuccess = true

        install(ContentNegotiation) { json(json) }

        install(HttpTimeout) {
            // Deliberately short for a feed request. A viewer staring at a
            // spinner for thirty seconds has already closed the app; failing at
            // ten and letting the pager retry is the better outcome.
            requestTimeoutMillis = 10_000
            connectTimeoutMillis = 5_000
            socketTimeoutMillis = 10_000
        }

        install(Auth) {
            bearer {
                loadTokens {
                    tokens.load()?.let { BearerTokens(it.access, it.refresh) }
                }
                refreshTokens {
                    val current = tokens.load() ?: return@refreshTokens null
                    runCatching {
                        // `client` here is the *unauthenticated* client Ktor
                        // hands the block, so the refresh call cannot recurse
                        // into another refresh.
                        val refreshed: TokenPair = client.post("$baseUrl/auth/refresh") {
                            markAsRefreshTokenRequest()
                            contentType(ContentType.Application.Json)
                            setBody(RefreshRequest(current.refresh))
                        }.body()
                        tokens.save(Tokens(refreshed.accessToken, refreshed.refreshToken))
                        BearerTokens(refreshed.accessToken, refreshed.refreshToken)
                    }.getOrElse {
                        // The refresh token is spent or revoked. Clearing here
                        // rather than leaving it means the next launch starts as
                        // an anonymous viewer — which is a working state — instead
                        // of retrying a dead token on every request.
                        tokens.clear()
                        null
                    }
                }
            }
        }

        install(DefaultRequest)
        defaultRequest {
            url(baseUrl.removeSuffix("/") + "/")
            header(HttpHeaders.Accept, ContentType.Application.Json)
            header(HttpHeaders.AcceptLanguage, identity.locale)
            // Namespaces the seen-set and the interest profile for an anonymous
            // viewer. Phase 4 keys interest profiles on `viewer_key`, which is
            // `d:<device>` until sign-in — so a device id that changes on every
            // launch means personalisation never accumulates.
            header("X-Device-Id", identity.deviceId)
        }

        HttpResponseValidator {
            handleResponseExceptionWithRequest { cause, _ ->
                val response = (cause as? ResponseException)?.response ?: throw cause
                throw response.toApiException()
            }
        }
        }

        return if (engine != null) HttpClient(engine, config) else HttpClient(config)
    }

    /**
     * A bare client for presigned object-store PUTs. Deliberately not [create].
     *
     * Two things about the API client are actively wrong for this job.
     *
     * **The `Auth` plugin.** A presigned URL carries its own signature in the
     * query string. Attaching `Authorization: Bearer …` to it makes S3 read the
     * request as an attempt at SigV4 *header* authentication, and it rejects it.
     * The symptom is a 403 on upload with a token that works everywhere else,
     * and it is a genuinely hard afternoon to diagnose.
     *
     * **The ten-second timeout.** That number was chosen for a feed page, where
     * failing fast and retrying beats a viewer watching a spinner. A twenty-
     * megabyte video on a congested cell will not finish in ten seconds, and the
     * client would abort it over and over with no error a seller could act on.
     *
     * No [ContentNegotiation] either: the body is bytes and the response is
     * empty. And no `expectSuccess` — the store is outside the problem+json
     * contract, so [ae.marketplace.mobile.sell.SellApi.uploadVideo] checks the
     * status itself rather than letting a validator invent an [ApiException] for
     * a response that has no problem document in it.
     */
    fun createUploader(engine: HttpClientEngine? = null): HttpClient {
        val config: HttpClientConfig<*>.() -> Unit = {
            install(HttpTimeout) {
                // Ten minutes end to end, matching the two-hour presign loosely:
                // long enough for a bad connection to finish a sixty-second clip,
                // short enough that a dead socket does not hold the coroutine for
                // the life of the process.
                requestTimeoutMillis = 10 * 60 * 1000
                connectTimeoutMillis = 15_000
                // Per-read, not total. A stalled upload is detected in half a
                // minute rather than in ten.
                socketTimeoutMillis = 30_000
            }
        }
        return if (engine != null) HttpClient(engine, config) else HttpClient(config)
    }
}

/**
 * Turns any non-2xx into an [ApiException], parsing problem+json when there is
 * one.
 *
 * `runCatching` around the body parse rather than a check on Content-Type: a
 * gateway timeout or a proxy 502 has no JSON at all, and that is still an error
 * worth throwing — it just has no structured detail to carry.
 */
private suspend fun HttpResponse.toApiException(): ApiException {
    val problem = runCatching { body<Problem>() }.getOrNull()
    return ApiException(status.value, problem, headers["X-Request-Id"])
}

@Serializable
private data class RefreshRequest(@SerialName("refresh_token") val refreshToken: String)

@Serializable
private data class TokenPair(
    @SerialName("access_token") val accessToken: String,
    @SerialName("refresh_token") val refreshToken: String,
)
