package ae.marketplace.mobile.push

import ae.marketplace.mobile.identity.Session
import ae.marketplace.mobile.identity.isAuthenticated
import io.ktor.client.HttpClient
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Hands the phone's FCM/APNs token to the server, against the signed-in
 * session. The OS hands out a token whether or not anyone is signed in, so it
 * is held here and sent once there is a session to attach it to.
 */
class PushRegistry(
    private val client: HttpClient,
    private val session: Session,
    private val scope: CoroutineScope,
) {
    private val lock = Mutex()
    private var token: String? = null
    private var platform: String = ""
    private var sent: String? = null
    private var retry: Job? = null

    init {
        scope.launch {
            session.state.collect { state ->
                if (state.isAuthenticated) flush() else lock.withLock { sent = null }
            }
        }
    }

    fun update(token: String, platform: String) {
        scope.launch {
            lock.withLock {
                this@PushRegistry.token = token
                this@PushRegistry.platform = platform
            }
            flush()
        }
    }

    /** Ends the server session, which also detaches this phone's token. */
    suspend fun logout() {
        runCatching { client.post("auth/logout") }
    }

    /**
     * Call when the app comes to the foreground or the network returns: retries
     * a token that has not reached the server yet.
     */
    fun retryNow() {
        scope.launch { flush() }
    }

    /**
     * Sends the token, and on failure (offline or 5xx at sign-in) keeps
     * retrying with backoff while the session lasts and the token is unsent.
     * Dropping it would leave push off until the next sign-in.
     */
    private suspend fun flush() {
        val ok = send() ?: return
        if (ok) {
            retry?.cancel()
            return
        }
        if (retry?.isActive == true) return
        retry = scope.launch {
            var attempt = 0
            while (true) {
                delay(retryDelayMs(attempt++))
                if (send() != false) return@launch
            }
        }
    }

    /** True when sent, false when it failed, null when there is nothing to send. */
    private suspend fun send(): Boolean? = lock.withLock {
        val t = token ?: return@withLock null
        if (!session.state.value.isAuthenticated || sent == t) return@withLock null
        runCatching {
            client.put("me/device") {
                contentType(ContentType.Application.Json)
                setBody(DeviceBody(t, platform))
            }
        }.onSuccess { sent = t }.isSuccess
    }
}

/** 2s, 4s, 8s … capped at five minutes. */
internal fun retryDelayMs(attempt: Int): Long =
    (2_000L shl attempt.coerceIn(0, 8)).coerceAtMost(300_000L)

@Serializable
private data class DeviceBody(
    @SerialName("push_token") val pushToken: String,
    val platform: String,
)
